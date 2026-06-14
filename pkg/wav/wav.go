// Package wav provides a minimal reader and writer for PCM WAV files.
// Reading supports 16, 24, and 32-bit mono PCM; all are decoded to int16.
// Writing always produces 16-bit mono PCM.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/internal/limits"
)

const (
	riffID = "RIFF"
	waveID = "WAVE"
	fmtID  = "fmt "
	dataID = "data"

	pcmFormat      = 1
	monoChannels   = 1
	stereoChannels = 2
	bitsPerSample  = 16

	smplChunkID         = "smpl"
	smplHeaderSize      = 36
	smplLoopRecordSize  = 24
	maxFmtChunkSize     = 1024
	minFmtChunkSize     = 16
	smplNumLoopsOffset  = 28
	smplLoopStartOffset = smplHeaderSize + 8
	smplLoopEndOffset   = smplHeaderSize + 12

	outputBytesPerSample = 2
	maxWAVSamples        = math.MaxInt32 / outputBytesPerSample

	// wavHeaderSize is the RIFF file size field value for a headeronly WAV
	// (no audio data): total header bytes (44) minus the 8-byte RIFF preamble.
	// Used to compute the FileSize field: wavHeaderSize + dataSize + smplChunkSize.
	wavHeaderSize = 36

	// signBit24 is the sign bit position for 24-bit PCM samples,
	// used during sign extension when decoding 24-bit audio to int16.
	signBit24 = 0x800000

	// mask24 is the 24-bit bitmask used for sign extension
	// when decoding 24-bit PCM audio to int16.
	mask24 = 0xFFFFFF

	nanosPerSecond      = 1_000_000_000
	smplPeriodOffset    = 8
	smplUnityNoteOffset = 12
	smplUnityNote       = 60
	loopRecStartOffset  = 8
	loopRecEndOffset    = 12
)

// Sentinel errors. Wrap with %w; callers should use errors.Is to identify
// a specific failure mode rather than matching error message substrings.
var (
	ErrNotRIFF        = errors.New("wav: not a RIFF file")
	ErrNotWAVE        = errors.New("wav: not a WAVE file")
	ErrDataBeforeFmt  = errors.New("wav: data chunk before fmt chunk")
	ErrMissingFmt     = errors.New("wav: no fmt chunk found")
	ErrMissingData    = errors.New("wav: no data chunk found")
	ErrUnsupportedPCM = errors.New("wav: unsupported PCM format")
	ErrSampleRate     = errors.New("wav: invalid sample rate")
	ErrBitDepth       = errors.New("wav: unsupported bit depth")
	ErrChannelCount   = errors.New("wav: unsupported channel count")
	ErrNoSamples      = errors.New("wav: no samples")
	ErrTooManySamples = errors.New("wav: too many samples")
	ErrDataTooLarge   = errors.New("wav: data chunk too large")
	ErrFmtChunkSize   = errors.New("wav: invalid fmt chunk size")
	ErrChunkSize      = errors.New("wav: chunk size out of range")
)

// File holds the decoded contents of a mono PCM WAV file.
// LoopStart and LoopEnd are sample indices from the SMPL chunk, or -1 if
// no loop is defined in the file. MIDIUnityNote is the SMPL chunk's root
// MIDI note (0..127); zero means "unset" and falls back to middle C (60)
// when writing, mirroring the legacy default.
//
// For stereo input, Samples holds interleaved frames (L0, R0, L1,
// R1, ...) and Channels reports 2. Mono input populates Samples
// directly with Channels=1. Callers that want a single channel from
// a stereo file use ExtractChannel or MixChannels.
type File struct {
	SampleRate    uint32
	Samples       []int16
	Channels      uint16
	LoopStart     int
	LoopEnd       int
	MIDIUnityNote uint8
}

// ExtractChannel returns a mono []int16 holding the requested
// channel (0=left, 1=right) of an interleaved stereo File. For a
// mono File, Samples is returned unchanged regardless of channel.
// Out-of-range channel indices return nil.
func (f *File) ExtractChannel(channel int) []int16 {
	if f.Channels <= 1 {
		return f.Samples
	}
	if channel < 0 || channel >= int(f.Channels) {
		return nil
	}
	stride := int(f.Channels)
	out := make([]int16, len(f.Samples)/stride)
	for i := range out {
		out[i] = f.Samples[i*stride+channel]
	}
	return out
}

// MixChannels returns a mono []int16 holding the per-frame average
// of every channel in a stereo File. For mono input Samples is
// returned unchanged. Average is computed in int32 to avoid
// overflow.
func (f *File) MixChannels() []int16 {
	if f.Channels <= 1 {
		return f.Samples
	}
	stride := int(f.Channels)
	out := make([]int16, len(f.Samples)/stride)
	for i := range out {
		var sum int32
		for c := 0; c < stride; c++ {
			sum += int32(f.Samples[i*stride+c])
		}
		out[i] = int16(sum / int32(stride)) //nolint:gosec // G115: stride is int(f.Channels) where f.Channels is uint16 (max 65535), well within int32 range.
	}
	return out
}

// Duration returns the duration in seconds.
func (f *File) Duration() float64 {
	if f.SampleRate == 0 {
		return 0
	}
	return float64(len(f.Samples)) / float64(f.SampleRate)
}

// Read decodes a WAV file from r. Supported formats: 16, 24, and 32-bit
// mono OR stereo PCM; all are decoded to int16. For stereo input, the
// returned File holds interleaved frames in Samples and Channels=2;
// callers select / mix channels via ExtractChannel / MixChannels.
// The reader is limited to 256 MB to prevent unbounded memory
// allocation on untrusted input.
func Read(r io.Reader) (*File, error) {
	lr := &io.LimitedReader{R: r, N: limits.MaxRead}

	var riffHdr [12]byte
	if _, err := io.ReadFull(lr, riffHdr[:]); err != nil {
		return nil, fmt.Errorf("wav: reading RIFF header: %w", err)
	}
	if string(riffHdr[0:4]) != riffID {
		return nil, ErrNotRIFF
	}
	if string(riffHdr[8:12]) != waveID {
		return nil, ErrNotWAVE
	}

	var sampleRate uint32
	var bitsField uint32
	var channels uint16
	foundFmt := false
	var samples []int16
	loopStart := -1
	loopEnd := -1
	var midiUnityNote uint8

	for {
		var chunkHdr [8]byte
		if _, err := io.ReadFull(lr, chunkHdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, fmt.Errorf("wav: reading chunk header: %w", err)
		}
		id := string(chunkHdr[0:4])
		size := binary.LittleEndian.Uint32(chunkHdr[4:8])
		// Reject pathological chunk sizes up-front: the LimitedReader caps us
		// at 256 MB anyway, and anything larger guarantees we cannot satisfy
		// the chunk. Returning a clean error here also avoids uint32 overflow
		// when computing the padded length below (0xFFFFFFFF + 1 wraps to 0
		// and causes misaligned parsing of subsequent chunks).
		if size > limits.MaxRead {
			return nil, fmt.Errorf("%w: chunk %q size %d exceeds limit", ErrChunkSize, id, size)
		}

		switch id {
		case fmtID:
			if size < minFmtChunkSize {
				return nil, fmt.Errorf("%w: too small (%d bytes)", ErrFmtChunkSize, size)
			}
			if size > maxFmtChunkSize {
				return nil, fmt.Errorf("%w: too large (%d bytes)", ErrFmtChunkSize, size)
			}
			fmtData := make([]byte, size)
			if _, err := io.ReadFull(lr, fmtData); err != nil {
				return nil, fmt.Errorf("wav: reading fmt chunk: %w", err)
			}
			if err := skipPadding(lr, id, size); err != nil {
				return nil, err
			}
			var err error
			sampleRate, bitsField, channels, err = parseFmtChunk(fmtData)
			if err != nil {
				return nil, err
			}
			foundFmt = true

		case dataID:
			if !foundFmt {
				return nil, ErrDataBeforeFmt
			}
			if size > limits.MaxRead {
				return nil, ErrDataTooLarge
			}
			raw := make([]byte, size)
			if _, err := io.ReadFull(lr, raw); err != nil {
				return nil, fmt.Errorf("wav: reading data chunk: %w", err)
			}
			if err := skipPadding(lr, id, size); err != nil {
				return nil, err
			}
			samples = parseDataChunk(raw, bitsField)

		case smplChunkID:
			if size < smplHeaderSize {
				padded := uint64(size)
				if padded%2 != 0 {
					padded++
				}
				if _, err := io.CopyN(io.Discard, lr, int64(padded)); err != nil {
					return nil, fmt.Errorf("wav: skipping smpl chunk: %w", err)
				}
				continue
			}
			smplData := make([]byte, size)
			if _, err := io.ReadFull(lr, smplData); err != nil {
				return nil, fmt.Errorf("wav: reading smpl chunk: %w", err)
			}
			if err := skipPadding(lr, id, size); err != nil {
				return nil, err
			}
			loopStart, loopEnd, midiUnityNote = parseSmplChunk(smplData, size)

		default:
			padded := uint64(size)
			if padded%2 != 0 {
				padded++
			}
			if _, err := io.CopyN(io.Discard, lr, int64(padded)); err != nil {
				return nil, fmt.Errorf("wav: skipping chunk %q: %w", id, err)
			}
		}
	}

	if !foundFmt {
		return nil, ErrMissingFmt
	}
	if samples == nil {
		return nil, ErrMissingData
	}
	if len(samples) == 0 {
		return nil, ErrNoSamples
	}
	// Validate SMPL loop points against the actual sample count. A bad
	// loop record should not break the whole import: clear the fields and
	// log a warning so callers see a clean (LoopStart, LoopEnd) = (-1, -1)
	// instead of out-of-range indices that downstream code must each guard
	// against. parseSmplChunk has already normalised inverted or zero-length
	// loops to (-1, -1); we only need to check the upper bound here.
	if loopStart >= 0 && loopEnd >= 0 {
		if loopStart >= len(samples) || loopEnd > len(samples) || loopStart >= loopEnd {
			log.Warn().
				Int("loop_start", loopStart).
				Int("loop_end", loopEnd).
				Int("samples", len(samples)).
				Msg("wav: ignoring malformed SMPL loop")
			loopStart = -1
			loopEnd = -1
		}
	}
	return &File{
		SampleRate:    sampleRate,
		Samples:       samples,
		Channels:      channels,
		LoopStart:     loopStart,
		LoopEnd:       loopEnd,
		MIDIUnityNote: midiUnityNote,
	}, nil
}

func parseFmtChunk(data []byte) (sampleRate uint32, bitsPerSample uint32, channels uint16, err error) {
	audioFmt := binary.LittleEndian.Uint16(data[0:2])
	channels = binary.LittleEndian.Uint16(data[2:4])
	sampleRate = binary.LittleEndian.Uint32(data[4:8])
	bits := binary.LittleEndian.Uint16(data[14:16])
	if audioFmt != pcmFormat {
		return 0, 0, 0, fmt.Errorf("%w: format %d (only PCM supported)", ErrUnsupportedPCM, audioFmt)
	}
	if channels != monoChannels && channels != stereoChannels {
		return 0, 0, 0, fmt.Errorf("%w: %d (supported: 1, 2)", ErrChannelCount, channels)
	}
	if bits != 16 && bits != 24 && bits != 32 {
		return 0, 0, 0, fmt.Errorf("%w: %d (supported: 16, 24, 32)", ErrBitDepth, bits)
	}
	if sampleRate == 0 {
		return 0, 0, 0, fmt.Errorf("%w: must be non-zero", ErrSampleRate)
	}
	return sampleRate, uint32(bits), channels, nil
}

func parseDataChunk(raw []byte, bitsPerSample uint32) []int16 {
	bytesPerSample := int(bitsPerSample / 8)
	nSamples := len(raw) / bytesPerSample
	samples := make([]int16, nSamples)
	for i := range samples {
		samples[i] = readSample(raw[i*bytesPerSample:], bytesPerSample)
	}
	return samples
}

func parseSmplChunk(data []byte, size uint32) (loopStart, loopEnd int, midiUnityNote uint8) {
	// midiUnityNote occupies the smpl header even when there are no loop
	// records, so read it whenever the header is fully present.
	unity := binary.LittleEndian.Uint32(data[smplUnityNoteOffset : smplUnityNoteOffset+4])
	if unity <= 127 {
		midiUnityNote = uint8(unity)
	}
	numLoops := binary.LittleEndian.Uint32(data[smplNumLoopsOffset : smplNumLoopsOffset+4])
	if numLoops > 0 && size >= smplHeaderSize+smplLoopRecordSize {
		loopStart = int(binary.LittleEndian.Uint32(data[smplLoopStartOffset : smplLoopStartOffset+4]))
		loopEnd = int(binary.LittleEndian.Uint32(data[smplLoopEndOffset : smplLoopEndOffset+4]))
		// Normalise degenerate loops (zero-length or inverted) to the
		// no-loop sentinel so callers don't see a loop where the writer
		// would refuse to emit one.
		if loopEnd <= loopStart {
			return -1, -1, midiUnityNote
		}
		return loopStart, loopEnd, midiUnityNote
	}
	return -1, -1, midiUnityNote
}

// readSample reads one PCM sample of bytesPerSample bytes (2, 3, or 4) from b
// and returns it as a signed 16-bit value, preserving the sign and scaling to
// the int16 range.
func readSample(b []byte, bytesPerSample int) int16 {
	switch bytesPerSample {
	case 2:
		return bitconv.ReadInt16LE(b)
	case 3:
		// 24-bit little-endian, sign-extend to 32 bits, then scale to 16-bit.
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&signBit24 != 0 {
			v |= ^int32(mask24)
		}
		return int16(v >> 8) //nolint:gosec // G115: intentional 24-bit to 16-bit truncation
	case 4:
		v := int32(binary.LittleEndian.Uint32(b)) //nolint:gosec // G115: standard 32-bit PCM decode
		return int16(v >> 16)
	default:
		return 0
	}
}

func skipPadding(r io.Reader, id string, size uint32) error {
	if size%2 != 0 {
		if _, err := io.CopyN(io.Discard, r, 1); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("wav: skipping padding after %s chunk: %w", id, err)
		}
	}
	return nil
}

// riffHeader is the fixed-layout RIFF/WAVE/fmt/data header for a 16-bit mono PCM file.
type riffHeader struct {
	RiffID   [4]byte
	FileSize uint32
	WaveID   [4]byte
	FmtID    [4]byte
	FmtSize  uint32
	Format   uint16
	Channels uint16
	Rate     uint32
	ByteRate uint32
	Align    uint16
	Bits     uint16
	DataID   [4]byte
	DataSize uint32
}

// Write encodes f as a 16-bit mono PCM WAV file and writes it to w.
// An SMPL chunk is written when EITHER a usable loop is present
// (LoopStart >= 0 and LoopEnd > LoopStart) OR MIDIUnityNote is non-zero.
// In the no-loop case the SMPL chunk carries only the 36-byte header
// (NumSampleLoops = 0) so the root note still round-trips for one-shot
// voices that audio editors and samplers can honour.
func Write(w io.Writer, f *File) error {
	if len(f.Samples) == 0 {
		return ErrNoSamples
	}
	if f.SampleRate == 0 {
		return fmt.Errorf("%w: must be non-zero", ErrSampleRate)
	}
	if len(f.Samples) > maxWAVSamples {
		return ErrTooManySamples
	}
	// Stereo write is intentionally rejected. Read accepts stereo
	// and exposes ExtractChannel / MixChannels for callers to
	// reduce to mono; Write writes a mono RIFF header, so passing
	// a stereo File here would produce a malformed WAV (mono
	// header with 2x sample data). Callers must explicitly pick
	// or mix channels before calling Write.
	if f.Channels >= stereoChannels {
		return fmt.Errorf("%w: Write does not support stereo (channels=%d); use ExtractChannel or MixChannels first",
			ErrChannelCount, f.Channels)
	}

	hasLoop := f.LoopStart >= 0 && f.LoopEnd > f.LoopStart
	emitSmpl := hasLoop || f.MIDIUnityNote != 0
	var smplChunkSize uint32
	if emitSmpl {
		smplChunkSize = 8 + smplHeaderSize
		if hasLoop {
			smplChunkSize += smplLoopRecordSize
		}
	}

	dataSize := bitconv.NarrowU32(len(f.Samples) * outputBytesPerSample)
	hdr := riffHeader{
		RiffID:   [4]byte{'R', 'I', 'F', 'F'},
		FileSize: wavHeaderSize + dataSize + smplChunkSize,
		WaveID:   [4]byte{'W', 'A', 'V', 'E'},
		FmtID:    [4]byte{'f', 'm', 't', ' '},
		FmtSize:  minFmtChunkSize,
		Format:   pcmFormat,
		Channels: monoChannels,
		Rate:     f.SampleRate,
		ByteRate: f.SampleRate * monoChannels * bitsPerSample / 8,
		Align:    monoChannels * bitsPerSample / 8,
		Bits:     bitsPerSample,
		DataID:   [4]byte{'d', 'a', 't', 'a'},
		DataSize: dataSize,
	}
	if err := binary.Write(w, binary.LittleEndian, hdr); err != nil {
		return fmt.Errorf("wav: writing header: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, f.Samples); err != nil {
		return fmt.Errorf("wav: writing samples: %w", err)
	}
	if emitSmpl {
		if err := writeSmplChunk(w, f, hasLoop); err != nil {
			return err
		}
	}
	return nil
}

func writeSmplChunk(w io.Writer, f *File, hasLoop bool) error {
	chunkDataSize := uint32(smplHeaderSize)
	if hasLoop {
		chunkDataSize += smplLoopRecordSize
	}
	var chunkID [4]byte
	copy(chunkID[:], smplChunkID)
	if err := binary.Write(w, binary.LittleEndian, chunkID); err != nil {
		return fmt.Errorf("wav: writing smpl chunk ID: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, chunkDataSize); err != nil {
		return fmt.Errorf("wav: writing smpl chunk size: %w", err)
	}
	smplHdr := make([]byte, smplHeaderSize)
	period := nanosPerSecond / f.SampleRate
	binary.LittleEndian.PutUint32(smplHdr[smplPeriodOffset:smplPeriodOffset+4], period)
	// MIDIUnityNote=0 is the "unset" sentinel; fall back to middle C (60)
	// for back-compat with callers that haven't been updated to thread
	// the voice's root note. Note 0 (C-1) is a valid MIDI value but is so
	// unusual for a sampler root that conflating it with "unset" is
	// safer than emitting it silently. Note: when hasLoop is false we
	// only reach here because MIDIUnityNote != 0, so the sentinel branch
	// is unreachable in the no-loop path.
	unityNote := uint32(f.MIDIUnityNote)
	if unityNote == 0 {
		unityNote = smplUnityNote
	}
	binary.LittleEndian.PutUint32(smplHdr[smplUnityNoteOffset:smplUnityNoteOffset+4], unityNote)
	numLoops := uint32(0)
	if hasLoop {
		numLoops = 1
	}
	binary.LittleEndian.PutUint32(smplHdr[smplNumLoopsOffset:smplNumLoopsOffset+4], numLoops)
	if _, err := w.Write(smplHdr); err != nil {
		return fmt.Errorf("wav: writing smpl header: %w", err)
	}
	if !hasLoop {
		return nil
	}
	loopRec := make([]byte, smplLoopRecordSize)
	binary.LittleEndian.PutUint32(loopRec[loopRecStartOffset:loopRecStartOffset+4], bitconv.NarrowU32(f.LoopStart))
	binary.LittleEndian.PutUint32(loopRec[loopRecEndOffset:loopRecEndOffset+4], bitconv.NarrowU32(f.LoopEnd))
	if _, err := w.Write(loopRec); err != nil {
		return fmt.Errorf("wav: writing smpl loop record: %w", err)
	}
	return nil
}
