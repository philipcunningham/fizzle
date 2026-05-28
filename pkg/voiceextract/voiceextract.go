// Package voiceextract implements the fizzle voice extract command. It reads
// an FZ series voice file (.fzv) and writes the raw audio as a 16-bit mono WAV.
package voiceextract

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/wav"
	"github.com/rs/zerolog/log"
)

const msPerSecond = 1000

// Extract reads the FZV voice file at fzvPath and writes its audio as a
// 16-bit mono WAV file to wavPath. The output is written atomically.
func Extract(fzvPath, wavPath string) error {
	data, err := fzutil.ReadFZV(fzvPath)
	if err != nil {
		return fmt.Errorf("voiceextract: %w", err)
	}

	sampleRate, samples, err := Decode(data)
	if err != nil {
		return err
	}

	loopStart, loopEnd := decodeLoopPoints(data)
	rootNote := decodeRootNote(data)

	log.Info().
		Str("fzv", filepath.Base(fzvPath)).
		Str("wav", filepath.Base(wavPath)).
		Uint32("rate", sampleRate).
		Int("samples", len(samples)).
		Msg("extracting voice audio")
	f := &wav.File{SampleRate: sampleRate, Samples: samples, LoopStart: loopStart, LoopEnd: loopEnd, MIDIUnityNote: rootNote}
	buf := bytes.NewBuffer(make([]byte, 0, wavOutputSize(f)))
	if err := wav.Write(buf, f); err != nil {
		return fmt.Errorf("voiceextract: encoding WAV: %w", err)
	}
	if err := fileutil.WriteAtomic(wavPath, buf.Bytes()); err != nil {
		return fmt.Errorf("voiceextract: %w", err)
	}
	return nil
}

// ExtractPlayback reads the FZV voice file and writes a WAV containing only
// the generator playback range (genst to gened), matching what the FZ hardware
// actually plays on note-on. leadInMs prepends silence to compensate for USB
// DAC startup latency.
func ExtractPlayback(fzvPath, wavPath string, leadInMs int) error {
	data, err := fzutil.ReadFZV(fzvPath)
	if err != nil {
		return fmt.Errorf("voiceextract: %w", err)
	}

	sampleRate, samples, err := DecodePlaybackRange(data, 0)
	if err != nil {
		return err
	}

	if leadInMs > 0 && sampleRate > 0 {
		leadInSamples := int(sampleRate) * leadInMs / msPerSecond
		padded := make([]int16, leadInSamples+len(samples))
		copy(padded[leadInSamples:], samples)
		samples = padded
	}

	f := &wav.File{SampleRate: sampleRate, Samples: samples, LoopStart: -1, LoopEnd: -1}
	buf := bytes.NewBuffer(make([]byte, 0, wavOutputSize(f)))
	if err := wav.Write(buf, f); err != nil {
		return fmt.Errorf("voiceextract: encoding WAV: %w", err)
	}
	if err := fileutil.WriteAtomic(wavPath, buf.Bytes()); err != nil {
		return fmt.Errorf("voiceextract: %w", err)
	}
	return nil
}

// wavOutputSize estimates the byte count wav.Write produces for f: 44-byte
// RIFF/WAVE/fmt/data header + len(Samples)*2 PCM bytes + optional smpl chunk.
// The smpl chunk is 8 (chunkID+size) + 36 (header) + 24 (loop record) when a
// loop is present, or 8 + 36 when only the root note is set on a one-shot
// voice. Used to pre-size the encoding buffer so wav.Write never grows it.
func wavOutputSize(f *wav.File) int {
	const wavHeaderBytes = 44
	const smplHeaderBytes = 8 + 36
	const smplLoopRecordBytes = 24
	size := wavHeaderBytes + len(f.Samples)*disk.BytesPerSample
	hasLoop := f.LoopStart >= 0 && f.LoopEnd > f.LoopStart
	if hasLoop {
		size += smplHeaderBytes + smplLoopRecordBytes
	} else if f.MIDIUnityNote != 0 {
		size += smplHeaderBytes
	}
	return size
}

// decodeRootNote returns the voice's root MIDI note (byte 0xB0 in the FZV
// voice header), or 0 if the header is truncated. The caller treats 0 as
// the "unset" sentinel and falls back to middle C; this matches the WAV
// SMPL chunk's MIDIUnityNote convention.
func decodeRootNote(data []byte) uint8 {
	if len(data) < disk.VoiceKeyCentOffset+1 {
		return 0
	}
	return data[disk.VoiceKeyCentOffset]
}

func decodeLoopPoints(data []byte) (loopStart, loopEnd int) {
	if len(data) < disk.SectorSize {
		return -1, -1
	}
	loopSus := data[disk.VoiceLoopSusOffset]
	if loopSus >= disk.NoSustainLoop {
		return -1, -1
	}
	// loop_sus (0..7) selects which of the eight loopst/looped pairs is
	// the active sustain loop; only that pair drives the WAV SMPL chunk
	// loop. Earlier revisions hard-coded index 0, which exported wrong
	// loop points for any voice whose sustain pair was not the first.
	stOff := disk.VoiceLoopSt0Offset + int(loopSus)*4
	edOff := disk.VoiceLoopEd0Offset + int(loopSus)*4
	// Mask the loop-fine byte (upper 8 bits of loopst) and the skip flag
	// (MSB of looped) so third-party voices that set these flag bits don't
	// produce 4 GB phantom addresses.
	rawSt := binary.LittleEndian.Uint32(data[stOff : stOff+4])
	rawEd := binary.LittleEndian.Uint32(data[edOff : edOff+4])
	ls := int(disk.LoopStartAddress(rawSt))
	le := int(disk.LoopEndAddress(rawEd))
	if le <= ls {
		return -1, -1
	}
	return ls, le
}

func parseVoiceAudio(data []byte) (sampleRate uint32, audioData []byte, maxSamples uint32, err error) {
	if len(data) < disk.SectorSize {
		return 0, nil, 0, fmt.Errorf("voiceextract: FZV too small (%d bytes)", len(data))
	}
	rateIdx := data[disk.VoiceSampOffset]
	if int(rateIdx) >= disk.NumSampleRates() {
		return 0, nil, 0, fmt.Errorf("voiceextract: invalid sample rate index %d", rateIdx)
	}
	sampleRate = disk.SampleRate(rateIdx)
	audioData = data[disk.SectorSize:]
	maxSamples = bitconv.NarrowU32(len(audioData) / disk.BytesPerSample)
	return sampleRate, audioData, maxSamples, nil
}

// DecodePlaybackRange extracts only the generator playback window (genst to
// gened) from raw FZV data. This matches what the FZ hardware actually plays
// on note-on: samples before genst and after gened are not audible. If genst
// is 0 and gened covers the full wave, this returns the same result as Decode.
// The optional leadInSamples parameter prepends that many zero samples to the
// output, which compensates for USB DAC startup latency.
func DecodePlaybackRange(data []byte, leadInSamples int) (sampleRate uint32, samples []int16, err error) {
	sampleRate, audioData, maxSamples, err := parseVoiceAudio(data)
	if err != nil {
		return 0, nil, err
	}

	genStart := binary.LittleEndian.Uint32(data[disk.VoiceGenStartOffset : disk.VoiceGenStartOffset+4])
	genEnd := binary.LittleEndian.Uint32(data[disk.VoiceGenEndOffset : disk.VoiceGenEndOffset+4])

	if genEnd == 0 {
		genEnd = maxSamples
	}
	if genStart > maxSamples {
		genStart = maxSamples
	}
	if genEnd > maxSamples {
		genEnd = maxSamples
	}
	if genEnd <= genStart {
		return sampleRate, nil, nil
	}

	n := genEnd - genStart
	samples = make([]int16, leadInSamples+int(n))
	for i := uint32(0); i < n; i++ {
		off := (genStart + i) * disk.BytesPerSample
		samples[leadInSamples+int(i)] = bitconv.ReadInt16LE(audioData[off : off+disk.BytesPerSample])
	}

	return sampleRate, samples, nil
}

// Decode extracts the sample rate and PCM samples from raw FZV data. It is
// exported so the studio TUI and integration tests can reuse it without
// writing intermediate files.
func Decode(data []byte) (sampleRate uint32, samples []int16, err error) {
	sampleRate, audioData, maxSamples, err := parseVoiceAudio(data)
	if err != nil {
		return 0, nil, err
	}

	waveStart := binary.LittleEndian.Uint32(data[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
	waveEnd := binary.LittleEndian.Uint32(data[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])

	if waveStart != 0 {
		return 0, nil, fmt.Errorf("voiceextract: invalid voice header (wave start = %d). Try 'fzf unpack' if this is a full dump file", waveStart)
	}

	if waveEnd == 0 && maxSamples == 0 {
		return 0, nil, errors.New("voiceextract: voice contains no audio samples")
	}
	if waveEnd == 0 {
		waveEnd = maxSamples
	}
	if waveEnd > maxSamples {
		return 0, nil, fmt.Errorf("voiceextract: wave end %d exceeds available samples %d", waveEnd, maxSamples)
	}

	samples = make([]int16, waveEnd)
	for i := uint32(0); i < waveEnd; i++ {
		samples[i] = bitconv.ReadInt16LE(audioData[i*disk.BytesPerSample : i*disk.BytesPerSample+disk.BytesPerSample])
	}

	return sampleRate, samples, nil
}
