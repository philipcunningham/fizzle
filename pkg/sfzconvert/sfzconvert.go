// Package sfzconvert converts an SFZ instrument file into an FZ series full
// dump (.fzf). Each SFZ region becomes one FZ voice, with its key range,
// velocity range, and root key mapped into the bank sector.
//
// WAV files referenced by the SFZ are read and resampled internally.
// No intermediate .fzv files are required.
package sfzconvert

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/render"
	"github.com/philipcunningham/fizzle/pkg/sfz"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// rateLadder is the supported rates from highest to lowest quality.
var rateLadder = disk.SampleRatesSlice()

// ConvertDir reads all WAV files from dirPath (sorted alphabetically), assigns
// each one to a sequential MIDI key starting at C2 (MIDI 36), and writes a
// full dump to outputPath. This is the zero-SFZ workflow for simple drum kits.
// The context is checked between WAV loads so a long convert can be cancelled.
func ConvertDir(ctx context.Context, dirPath, outputPath string, targetRate uint32, fitToDisk bool) error {
	if err := disk.ValidateRate(targetRate); err != nil {
		return fmt.Errorf("sfzconvert: %w", err)
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("sfzconvert: reading directory %q: %w", dirPath, err)
	}

	wavPaths := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".wav") {
			wavPaths = append(wavPaths, filepath.Join(dirPath, e.Name()))
		}
	}
	sort.Strings(wavPaths)

	if len(wavPaths) == 0 {
		subCount := countSubdirWAVs(dirPath)
		if subCount > 0 {
			return fmt.Errorf("sfzconvert: no WAV files found in %q (found %d in subdirectories; move them to the top level to convert)", dirPath, subCount)
		}
		return fmt.Errorf("sfzconvert: no WAV files found in %q", dirPath)
	}
	if len(wavPaths) > disk.MaxVoices {
		return fmt.Errorf("sfzconvert: %d WAV files exceeds maximum of %d voices", len(wavPaths), disk.MaxVoices)
	}

	log.Info().
		Str("dir", filepath.Base(dirPath)).
		Int("files", len(wavPaths)).
		Msg("converting WAV directory")

	// Build synthetic regions: one per WAV, sequential keys from C2 (MIDI 36).
	// NewRegion seeds the optional opcodes (cutoff, resonance, loop_start,
	// loop_end) with the "absent" sentinel so regionToFZVFromFile leaves the
	// hardware defaults from voiceimport.Encode in place.
	regions := make([]sfz.Region, len(wavPaths))
	for i, p := range wavPaths {
		note := uint8(disk.FirstMIDINote + i)
		r := sfz.NewRegion()
		r.Sample = p
		r.LoKey = note
		r.HiKey = note
		r.PitchKeycenter = note
		regions[i] = r
	}

	// Load WAVs.
	wavFiles := make(map[string]*wav.File, len(wavPaths))
	for i, p := range wavPaths {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sfzconvert: %w", err)
		}
		f, err := fzutil.ReadWAV(p)
		if err != nil {
			return fmt.Errorf("sfzconvert: %s: %w", filepath.Base(p), err)
		}
		wavFiles[p] = f
		log.Debug().
			Str("n", fmt.Sprintf("%d/%d", i+1, len(wavPaths))).
			Str("file", filepath.Base(p)).
			Msg("loaded WAV")
	}

	return convertRegions(ctx, regions, wavFiles, outputPath, targetRate, fitToDisk)
}

// ConvertMultiDisk converts an SFZ file to a 2-disk full dump, writing
// outputPrefix-1.img and outputPrefix-2.img. Disk 1 contains the complete
// bank and voice headers plus the first portion of audio. Disk 2 contains
// pure audio continuation (no bank or voice headers), matching the format
// the hardware writes when saving a multi-disk instrument. The context is
// checked between WAV loads.
func ConvertMultiDisk(ctx context.Context, sfzPath, outputPrefix string, targetRate uint32) error {
	regions, wavFiles, err := parseSFZAndLoadWAVs(ctx, sfzPath, targetRate)
	if err != nil {
		return err
	}

	rateIdx, _ := disk.RateIndexFor(targetRate)
	log.Info().
		Int("count", len(regions)).
		Uint32("rate", targetRate).
		Msg("converting regions")

	voices, keygroups, err := convertVoices(ctx, regions, wavFiles, rateIdx, targetRate)
	if err != nil {
		return err
	}

	result, err := voicebuild.AssembleMultiDisk(voices, keygroups)
	if err != nil {
		var tmd *voicebuild.ErrTooManyDisks
		if errors.As(err, &tmd) {
			return fmt.Errorf("%w\nuse --fit-to-disk instead to downsample automatically and fit on a single disk", err)
		}
		var ram *voicebuild.ErrSampleRAMExceeded
		if errors.As(err, &ram) {
			return fmt.Errorf("%w\ntrim or shorten samples to fit within the sampler's 2 MB sample memory", err)
		}
		return fmt.Errorf("sfzconvert: assembling multi-disk dump: %w", err)
	}

	baseName := filepath.Base(outputPrefix)
	name := disk.PadLabel(disk.FullDumpName)

	for i, d := range result.Disks {
		diskNum := uint8(i)
		label := baseName + fmt.Sprintf(" %d", i+1)
		if len(label) > disk.LabelSize {
			label = label[:disk.LabelSize]
		}
		imgPath := fmt.Sprintf("%s-%d.img", outputPrefix, i+1)

		if err := diskformat.Format(imgPath, label); err != nil {
			return fmt.Errorf("sfzconvert: formatting disk %d: %w", i+1, err)
		}
		if err := diskadd.AddBytes(imgPath, d, name, disk.TypeFullDump, diskNum, result.BankCount, result.VoiceCount, result.WaveCount); err != nil {
			return fmt.Errorf("sfzconvert: adding data to disk %d: %w", i+1, err)
		}

		log.Info().
			Str("file", filepath.Base(imgPath)).
			Str("size", render.FormatBytes(len(d))).
			Msg("writing disk image")
	}
	return nil
}

// Convert reads the SFZ file at sfzPath, converts all regions to FZ voices,
// and writes a full dump to outputPath. targetRate must be 36000, 18000, or
// 9000. If fitToDisk is true the rate is automatically stepped down from
// targetRate to ensure the output fits on a single floppy disk; an error is
// returned if even 9000 Hz is too large. The context is checked between
// WAV loads so a long convert can be cancelled.
func Convert(ctx context.Context, sfzPath, outputPath string, targetRate uint32, fitToDisk bool) error {
	regions, wavFiles, err := parseSFZAndLoadWAVs(ctx, sfzPath, targetRate)
	if err != nil {
		return err
	}
	return convertRegions(ctx, regions, wavFiles, outputPath, targetRate, fitToDisk)
}

func parseSFZAndLoadWAVs(ctx context.Context, sfzPath string, targetRate uint32) ([]sfz.Region, map[string]*wav.File, error) {
	if err := disk.ValidateRate(targetRate); err != nil {
		return nil, nil, fmt.Errorf("sfzconvert: %w", err)
	}
	log.Info().Str("file", filepath.Base(sfzPath)).Msg("parsing SFZ")
	regions, warns, err := sfz.Parse(sfzPath)
	if err != nil {
		return nil, nil, fmt.Errorf("sfzconvert: parsing SFZ: %w", err)
	}
	for _, warn := range warns {
		e := log.Warn()
		if warn.Region >= 0 {
			e = e.Int("region", warn.Region+1)
		}
		e.Msg(warn.Message)
	}
	log.Debug().Int("count", len(regions)).Msg("loading WAV files")
	wavFiles, err := loadWAVFiles(ctx, regions)
	if err != nil {
		return nil, nil, err
	}
	return regions, wavFiles, nil
}

func loadWAVFiles(ctx context.Context, regions []sfz.Region) (map[string]*wav.File, error) {
	wavFiles := make(map[string]*wav.File, len(regions))
	for i, r := range regions {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("sfzconvert: %w", err)
		}
		if _, loaded := wavFiles[r.Sample]; loaded {
			continue
		}
		f, err := fzutil.ReadWAV(r.Sample)
		if err != nil {
			return nil, fmt.Errorf("sfzconvert: region %d (%s): %w", i+1, filepath.Base(r.Sample), err)
		}
		wavFiles[r.Sample] = f
		log.Debug().
			Str("n", fmt.Sprintf("%d/%d", i+1, len(regions))).
			Str("file", filepath.Base(r.Sample)).
			Msg("loaded WAV")
	}
	return wavFiles, nil
}

func convertVoices(ctx context.Context, regions []sfz.Region, wavFiles map[string]*wav.File, rateIdx uint8, targetRate uint32) ([][]byte, []voicebuild.Keygroup, error) {
	muteGroupToGen := buildMuteGroupMap(regions)
	voices := make([][]byte, len(regions))
	keygroups := make([]voicebuild.Keygroup, len(regions))
	for i, r := range regions {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("sfzconvert: %w", err)
		}
		log.Debug().
			Str("n", fmt.Sprintf("%d/%d", i+1, len(regions))).
			Str("sample", filepath.Base(r.Sample)).
			Msg("converting region")
		fzv, err := regionToFZVFromFile(r, wavFiles[r.Sample], rateIdx, targetRate)
		if err != nil {
			return nil, nil, fmt.Errorf("sfzconvert: region %d (%s): %w", i+1, filepath.Base(r.Sample), err)
		}
		voices[i] = fzv
		keygroups[i] = buildKeygroup(r, muteGroupToGen)
	}
	return voices, keygroups, nil
}

// buildMuteGroupMap assigns each unique mutegroup value to a generator bit (1-8).
// Only regions where HasMuteGroup=true are considered. Regions without the opcode
// are polyphonic regardless of any default value.
func buildMuteGroupMap(regions []sfz.Region) map[int]uint8 {
	muteGroupToGen := map[int]uint8{}
	nextGen := uint8(1)
	for _, r := range regions {
		if r.HasMuteGroup {
			if _, seen := muteGroupToGen[r.MuteGroup]; !seen {
				muteGroupToGen[r.MuteGroup] = nextGen
				nextGen++
				if nextGen > disk.MaxGenerators {
					log.Warn().Msg("instrument has more than 8 mute groups; groups beyond 8 share generator 8 and will mute each other")
					nextGen = disk.MaxGenerators
				}
			}
		}
	}
	return muteGroupToGen
}

// buildKeygroup constructs a Keygroup for a region using the mute group map.
// Regions with HasMuteGroup=true get a single-bit gchn (monophonic).
// Regions without mutegroup get gchn=0xff (polyphonic).
func buildKeygroup(r sfz.Region, muteGroupToGen map[int]uint8) voicebuild.Keygroup {
	kg := voicebuild.NewKeygroup(r.LoKey, r.HiKey, r.PitchKeycenter)
	kg.VelLow = r.LoVel
	kg.VelHigh = r.HiVel
	if r.HasMuteGroup {
		if gen, ok := muteGroupToGen[r.MuteGroup]; ok {
			kg.AudioOut = 1 << (gen - 1)
		}
	}
	return kg
}

// convertRegions is the shared implementation called by both Convert and ConvertDir.
func convertRegions(ctx context.Context, regions []sfz.Region, wavFiles map[string]*wav.File, outputPath string, targetRate uint32, fitToDisk bool) error {
	chosenRate, err := selectRate(regions, wavFiles, targetRate, fitToDisk)
	if err != nil {
		return err
	}
	rateIdx, _ := disk.RateIndexFor(chosenRate)

	log.Info().
		Int("count", len(regions)).
		Uint32("rate", chosenRate).
		Msg("converting regions")

	voices, keygroups, err := convertVoices(ctx, regions, wavFiles, rateIdx, chosenRate)
	if err != nil {
		return err
	}

	out, err := voicebuild.AssembleWithKeygroups(voices, keygroups)
	if err != nil {
		return fmt.Errorf("sfzconvert: assembling dump: %w", err)
	}
	log.Info().
		Str("file", filepath.Base(outputPath)).
		Str("size", render.FormatBytes(len(out))).
		Msg("writing full dump")
	if fzutil.OverCapacity(len(out)) {
		log.Warn().
			Str("size", render.FormatBytes(len(out))).
			Str("limit", render.FormatBytes(disk.UsableDataSize)).
			Msg("voice data exceeds floppy disk capacity")
	}
	return fileutil.WriteAtomic(outputPath, out)
}

// selectRate returns the encoding rate to use. If fitToDisk is false it
// returns targetRate unchanged. If fitToDisk is true it walks down the rate
// ladder from targetRate and returns the first rate whose estimated output
// fits within disk.UsableDataSize. It logs a WARN if the rate is stepped down.
func selectRate(regions []sfz.Region, wavFiles map[string]*wav.File, targetRate uint32, fitToDisk bool) (uint32, error) {
	if !fitToDisk {
		return targetRate, nil
	}

	for _, rate := range rateLadder {
		if rate > targetRate {
			continue
		}
		est := estimateFZFSize(regions, wavFiles, rate)
		log.Debug().
			Uint32("rate", rate).
			Str("estimated", render.FormatBytes(est)).
			Msg("size estimate")
		if est <= disk.UsableDataSize {
			if rate != targetRate {
				log.Warn().
					Str("requested", fmt.Sprintf("%d Hz", targetRate)).
					Str("using", fmt.Sprintf("%d Hz", rate)).
					Str("estimated", render.FormatBytes(est)).
					Msg("downsampling to fit on disk")
			}
			return rate, nil
		}
	}

	minEst := estimateFZFSize(regions, wavFiles, 9000)
	return 0, fmt.Errorf(
		"sfzconvert: instrument is too large for a floppy disk even at 9000 Hz (estimated %s, limit %s)",
		render.FormatBytes(minEst),
		render.FormatBytes(disk.UsableDataSize),
	)
}

// estimateFZFSize computes the approximate FZF output size in bytes for the
// given regions and rate without encoding any audio. It accounts for the bank
// sector, voice area, and per-voice audio blocks (sector-aligned).
func estimateFZFSize(regions []sfz.Region, wavFiles map[string]*wav.File, targetRate uint32) int {
	n := len(regions)
	bankSector := disk.SectorSize
	voiceSectors := disk.VoiceAreaSectors(n) * disk.SectorSize

	audioBytes := 0
	for _, r := range regions {
		f, ok := wavFiles[r.Sample]
		if !ok {
			continue
		}
		ratio := float64(targetRate) / float64(f.SampleRate)
		outSamples := int(math.Round(float64(len(f.Samples)) * ratio))
		rawBytes := disk.PadToSector(outSamples * disk.BytesPerSample)
		audioBytes += rawBytes
	}

	return bankSector + voiceSectors + audioBytes
}

// regionToFZVFromFile converts one SFZ region to a raw FZV byte slice using a
// pre-loaded WAV file. Loop points from the WAV SMPL chunk are scaled to the
// target sample rate and passed to Encode.
func regionToFZVFromFile(r sfz.Region, f *wav.File, rateIdx uint8, targetRate uint32) ([]byte, error) {
	samples, err := fzutil.Resample(f, targetRate)
	if err != nil {
		return nil, err
	}
	name := fzutil.VoiceName(r.Sample)

	loopStartSrc := f.LoopStart
	loopEndSrc := f.LoopEnd
	if r.LoopStart >= 0 && r.LoopEnd > r.LoopStart {
		loopStartSrc = r.LoopStart
		loopEndSrc = r.LoopEnd
	}

	loop := voiceimport.NoLoop()
	if !r.OneShot && loopStartSrc >= 0 && loopEndSrc > loopStartSrc && f.SampleRate > 0 {
		ratio := float64(targetRate) / float64(f.SampleRate)
		ls := int(math.Round(float64(loopStartSrc) * ratio))
		le := int(math.Round(float64(loopEndSrc) * ratio))
		if le > len(samples) {
			le = len(samples)
		}
		if ls < le {
			loop = voiceimport.LoopParams{LoopStart: ls, LoopEnd: le}
			log.Debug().
				Int("loop_start_src", loopStartSrc).
				Int("loop_end_src", loopEndSrc).
				Int("loop_start_fz", ls).
				Int("loop_end_fz", le).
				Msg("loop points scaled")
		}
	}

	fzv := voiceimport.Encode(samples, rateIdx, name, r.Transpose, loop)
	if r.Tune != 0 {
		currentDCP := int32(int16(binary.LittleEndian.Uint16(fzv[disk.VoiceDCPOffset:]))) //nolint:gosec // G115: intentional uint16-to-int16 reinterpretation for signed DCP value
		tuneDCP := int32(math.Round(float64(r.Tune) * 256.0 / 100.0))
		// Sum in int32 so the worst-case combination (e.g. transpose=127 +
		// tune=100 yields 32768) doesn't wrap into negative territory and
		// flip the pitch direction. Saturate at the int16 range and warn
		// so the user sees that the requested pitch was clipped.
		sumDCP := currentDCP + tuneDCP
		if sumDCP > math.MaxInt16 || sumDCP < math.MinInt16 {
			log.Warn().
				Int("transpose", r.Transpose).
				Int("tune", r.Tune).
				Int32("dcp", sumDCP).
				Str("sample", filepath.Base(r.Sample)).
				Msg("combined transpose+tune exceeds DCP range; clamping to int16")
			if sumDCP > math.MaxInt16 {
				sumDCP = math.MaxInt16
			} else {
				sumDCP = math.MinInt16
			}
		}
		binary.LittleEndian.PutUint16(fzv[disk.VoiceDCPOffset:], uint16(int16(sumDCP))) //nolint:gosec // G115: explicitly clamped to int16 range above
	}
	if r.Cutoff >= 0 {
		fzv[disk.VoiceDCFOffset] = uint8(r.Cutoff) //nolint:gosec // clamped to 0-127 by parser
	}
	if r.Resonance >= 0 {
		fzv[disk.VoiceDCQOffset] = uint8(r.Resonance) //nolint:gosec // clamped to 0-127 by parser
	}
	// Patch the FZV voice header's keynote centre (spec §2-1, offset 0xB0) so
	// the per-voice root key reflects the SFZ region's pitch_keycenter. Without
	// this, voiceimport.Encode leaves the header at DefaultKeyCentre (72) while
	// buildKeygroup writes the per-key bank sector cent[i], causing a
	// round-trip leak on fzv extract / sfz export (which read VoiceKeyCentOffset).
	fzv[disk.VoiceKeyCentOffset] = r.PitchKeycenter
	return fzv, nil
}

func countSubdirWAVs(dir string) int {
	count := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() && strings.HasSuffix(strings.ToLower(se.Name()), ".wav") {
				count++
			}
		}
	}
	return count
}
