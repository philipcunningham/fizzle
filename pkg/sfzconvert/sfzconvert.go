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
	"io/fs"
	"math"
	"os"
	"path"
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

// Result carries an in-memory conversion: the assembled full dump and the
// sample rate actually used, which fit to disk may have stepped down from
// the requested rate.
type Result struct {
	FZF  []byte
	Rate uint32
}

// ConvertFS converts the SFZ file at sfzPath inside fsys and returns the
// assembled full dump. It is the in-memory twin of Convert: same pipeline,
// same fit to disk behaviour, no filesystem writes. Sample references
// resolve inside fsys.
//
// channel answers for the whole instrument, as it does for
// ConvertDirFS: it says which side of a stereo sample the FZ keeps.
// voiceimport.ChannelMonoOnly refuses stereo input rather than
// guessing.
func ConvertFS(ctx context.Context, fsys fs.FS, sfzPath string, targetRate uint32, fitToDisk bool, channel voiceimport.Channel) (*Result, error) {
	regions, wavFiles, err := parseAndLoad(ctx, fsys, sfzPath, targetRate)
	if err != nil {
		return nil, err
	}
	if err := reduceToMono(wavFiles, channel); err != nil {
		return nil, err
	}
	return assembleSingle(ctx, regions, wavFiles, targetRate, fitToDisk)
}

// ConvertDirFS converts every WAV in fsys at any depth (sorted by
// path, sequential keys from C2) and returns the assembled full dump.
// It is the in-memory twin of ConvertDir. Depth is the one place the
// two differ.
//
// ConvertDir takes a directory path, so the WAVs below it may belong
// to a neighbouring library. A caller who meant them can move them
// up, and the refusal says so. fsys is the other case: it holds
// exactly the tree the browser user dropped, and the dialog has
// already counted all of it. Converting the top level alone would
// drop the rest in silence.
//
// channel answers for the whole folder: it says which side of a stereo
// file the FZ keeps. voiceimport.ChannelMonoOnly refuses stereo input
// rather than guessing, which is what ConvertDir's callers want.
func ConvertDirFS(ctx context.Context, fsys fs.FS, targetRate uint32, fitToDisk bool, channel voiceimport.Channel) (*Result, error) {
	if err := disk.ValidateRate(targetRate); err != nil {
		return nil, fmt.Errorf("sfzconvert: %w", err)
	}
	wavPaths, err := walkWAVsFS(fsys)
	if err != nil {
		return nil, err
	}
	sort.Strings(wavPaths)
	if len(wavPaths) == 0 {
		// The folder has no name here: fsys is rooted at what the user
		// dropped, and the walk has already looked everywhere below it.
		return nil, fmt.Errorf("sfzconvert: no WAV files found in the folder")
	}
	regions, err := sequentialRegions(wavPaths)
	if err != nil {
		return nil, err
	}
	wavFiles, err := loadWAVFiles(ctx, fsys, regions)
	if err != nil {
		return nil, err
	}
	if err := reduceToMono(wavFiles, channel); err != nil {
		return nil, err
	}
	return assembleSingle(ctx, regions, wavFiles, targetRate, fitToDisk)
}

// reduceToMono collapses every stereo file to the one channel the
// caller chose, in place. It runs before the rate is selected because
// the size estimate and the resampler both measure sample counts, and
// an interleaved stereo buffer holds two per frame. Without the
// reduction a stereo file becomes a voice of double length at half
// the pitch, with the channels alternating sample by sample.
//
// The channel is checked before any file is looked at, so an argument
// the package doesn't know is refused even when every file is mono.
func reduceToMono(wavFiles map[string]*wav.File, channel voiceimport.Channel) error {
	var reduce func(*wav.File) []int16
	switch channel {
	case voiceimport.ChannelLeft:
		reduce = func(f *wav.File) []int16 { return f.ExtractChannel(0) }
	case voiceimport.ChannelRight:
		reduce = func(f *wav.File) []int16 { return f.ExtractChannel(1) }
	case voiceimport.ChannelMix:
		reduce = func(f *wav.File) []int16 { return f.MixChannels() }
	case voiceimport.ChannelMonoOnly:
		reduce = nil // Stereo is refused below rather than guessed at.
	default:
		return fmt.Errorf("sfzconvert: invalid channel %d", channel)
	}
	for _, name := range sortedNames(wavFiles) {
		f := wavFiles[name]
		if f.Channels < 2 {
			continue
		}
		if reduce == nil {
			return fmt.Errorf("sfzconvert: %q is stereo; choose left, right, or mix", path.Base(name))
		}
		f.Samples = reduce(f)
		f.Channels = 1
	}
	return nil
}

// refuseStereo fails on the first stereo file rather than write a
// voice of double length at half the pitch. The three path mode entry
// points carry no channel answer, so naming the file and pointing at a
// mono conversion is the only remedy that exists, matching what
// fizzle fzv import does.
func refuseStereo(wavFiles map[string]*wav.File) error {
	for _, name := range sortedNames(wavFiles) {
		if wavFiles[name].Channels >= 2 {
			return fmt.Errorf("sfzconvert: %q is stereo and sfz convert writes mono only; convert it to mono first", name)
		}
	}
	return nil
}

// sortedNames orders the loaded files so a refusal always names the
// same one when several are stereo, rather than whichever the map
// handed over first.
func sortedNames(wavFiles map[string]*wav.File) []string {
	names := make([]string, 0, len(wavFiles))
	for name := range wavFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ConvertMultiDiskFS converts the SFZ file at sfzPath inside fsys into a
// two disk full dump. It is the in-memory twin of ConvertMultiDisk: the
// caller writes the returned disks. channel carries the same answer
// ConvertFS takes.
func ConvertMultiDiskFS(ctx context.Context, fsys fs.FS, sfzPath string, targetRate uint32, channel voiceimport.Channel) (voicebuild.MultiDiskResult, error) {
	regions, wavFiles, err := parseAndLoad(ctx, fsys, sfzPath, targetRate)
	if err != nil {
		return voicebuild.MultiDiskResult{}, err
	}
	if err := reduceToMono(wavFiles, channel); err != nil {
		return voicebuild.MultiDiskResult{}, err
	}
	return assembleMulti(ctx, regions, wavFiles, targetRate)
}

// sequentialRegions builds one region per sample path with sequential MIDI
// keys from C2 (MIDI 36), the zero SFZ drum kit layout.
func sequentialRegions(samplePaths []string) ([]sfz.Region, error) {
	if len(samplePaths) > disk.MaxVoices {
		return nil, fmt.Errorf("sfzconvert: %d WAV files exceeds maximum of %d voices", len(samplePaths), disk.MaxVoices)
	}
	regions := make([]sfz.Region, len(samplePaths))
	for i, p := range samplePaths {
		note := uint8(disk.FirstMIDINote + i) //nolint:gosec // G115: bounded by MaxVoices above
		r := sfz.NewRegion()
		r.Sample = p
		r.LoKey = note
		r.HiKey = note
		r.PitchKeycenter = note
		regions[i] = r
	}
	return regions, nil
}

// assembleSingle runs the shared single disk pipeline (rate selection,
// region conversion, assembly) and returns the dump bytes.
func assembleSingle(ctx context.Context, regions []sfz.Region, wavFiles map[string]*wav.File, targetRate uint32, fitToDisk bool) (*Result, error) {
	chosenRate, err := selectRate(regions, wavFiles, targetRate, fitToDisk)
	if err != nil {
		return nil, err
	}
	rateIdx, _ := disk.RateIndexFor(chosenRate)

	log.Info().
		Int("count", len(regions)).
		Uint32("rate", chosenRate).
		Msg("converting regions")

	voices, keygroups, err := convertVoices(ctx, regions, wavFiles, rateIdx, chosenRate)
	if err != nil {
		return nil, err
	}
	out, err := voicebuild.AssembleWithKeygroups(voices, keygroups)
	if err != nil {
		return nil, fmt.Errorf("sfzconvert: assembling dump: %w", err)
	}
	return &Result{FZF: out, Rate: chosenRate}, nil
}

// assembleMulti runs the shared two disk pipeline. It reports the
// region count and rate as assembleSingle does: the split is the
// slowest thing the CLI does, and this line is its only progress
// signal.
func assembleMulti(ctx context.Context, regions []sfz.Region, wavFiles map[string]*wav.File, targetRate uint32) (voicebuild.MultiDiskResult, error) {
	rateIdx, _ := disk.RateIndexFor(targetRate)

	log.Info().
		Int("count", len(regions)).
		Uint32("rate", targetRate).
		Msg("converting regions")

	voices, keygroups, err := convertVoices(ctx, regions, wavFiles, rateIdx, targetRate)
	if err != nil {
		return voicebuild.MultiDiskResult{}, err
	}
	result, err := voicebuild.AssembleMultiDisk(voices, keygroups)
	if err != nil {
		var tmd *voicebuild.ErrTooManyDisks
		if errors.As(err, &tmd) {
			return voicebuild.MultiDiskResult{}, fmt.Errorf("%w\nuse --fit-to-disk instead to downsample automatically and fit on a single disk", err)
		}
		var ram *voicebuild.ErrSampleRAMExceeded
		if errors.As(err, &ram) {
			return voicebuild.MultiDiskResult{}, fmt.Errorf("%w\ntrim or shorten samples to fit within the sampler's 2 MB sample memory", err)
		}
		return voicebuild.MultiDiskResult{}, fmt.Errorf("sfzconvert: assembling multi-disk dump: %w", err)
	}
	return result, nil
}

// ConvertDir reads all WAV files from dirPath (sorted alphabetically), assigns
// each one to a sequential MIDI key starting at C2 (MIDI 36), and writes a
// full dump to outputPath. This is the zero-SFZ workflow for simple drum kits.
// The context is checked between WAV loads so a long convert can be cancelled.
// A stereo WAV is refused: the command carries no channel answer, and a
// mangled voice is worse than a refusal that names the file.
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
	if err := refuseStereo(wavFiles); err != nil {
		return err
	}

	return convertRegions(ctx, regions, wavFiles, outputPath, targetRate, fitToDisk)
}

// ConvertMultiDisk converts an SFZ file to a 2-disk full dump, writing
// outputPrefix-1.img and outputPrefix-2.img. Disk 1 contains the complete
// bank and voice headers plus the first portion of audio. Disk 2 contains
// pure audio continuation (no bank or voice headers), matching the format
// the hardware writes when saving a multi-disk instrument. The context is
// checked between WAV loads. Stereo samples are refused, as they are in
// Convert.
func ConvertMultiDisk(ctx context.Context, sfzPath, outputPrefix string, targetRate uint32) error {
	regions, wavFiles, err := parseAndLoad(ctx, nil, sfzPath, targetRate)
	if err != nil {
		return err
	}
	if err := refuseStereo(wavFiles); err != nil {
		return err
	}

	result, err := assembleMulti(ctx, regions, wavFiles, targetRate)
	if err != nil {
		return err
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
// WAV loads so a long convert can be cancelled. A stereo sample is
// refused: the command carries no channel answer, so the alternative is
// a voice of double length at half the pitch.
func Convert(ctx context.Context, sfzPath, outputPath string, targetRate uint32, fitToDisk bool) error {
	regions, wavFiles, err := parseAndLoad(ctx, nil, sfzPath, targetRate)
	if err != nil {
		return err
	}
	if err := refuseStereo(wavFiles); err != nil {
		return err
	}
	return convertRegions(ctx, regions, wavFiles, outputPath, targetRate, fitToDisk)
}

// parseAndLoad parses the SFZ and reads every referenced WAV. A nil fsys
// reads from the host filesystem; otherwise everything resolves inside
// fsys.
func parseAndLoad(ctx context.Context, fsys fs.FS, sfzPath string, targetRate uint32) ([]sfz.Region, map[string]*wav.File, error) {
	if err := disk.ValidateRate(targetRate); err != nil {
		return nil, nil, fmt.Errorf("sfzconvert: %w", err)
	}
	log.Info().Str("file", filepath.Base(sfzPath)).Msg("parsing SFZ")
	var (
		regions []sfz.Region
		warns   []sfz.Warning
		err     error
	)
	if fsys == nil {
		regions, warns, err = sfz.Parse(sfzPath)
	} else {
		regions, warns, err = sfz.ParseFS(fsys, sfzPath)
	}
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
	wavFiles, err := loadWAVFiles(ctx, fsys, regions)
	if err != nil {
		return nil, nil, err
	}
	return regions, wavFiles, nil
}

func loadWAVFiles(ctx context.Context, fsys fs.FS, regions []sfz.Region) (map[string]*wav.File, error) {
	wavFiles := make(map[string]*wav.File, len(regions))
	for i, r := range regions {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("sfzconvert: %w", err)
		}
		if _, loaded := wavFiles[r.Sample]; loaded {
			continue
		}
		f, err := readWAV(fsys, r.Sample)
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

// readWAV reads one WAV from the host filesystem (nil fsys) or from fsys.
func readWAV(fsys fs.FS, samplePath string) (*wav.File, error) {
	if fsys == nil {
		return fzutil.ReadWAV(samplePath)
	}
	fh, err := fsys.Open(samplePath)
	if err != nil {
		return nil, fmt.Errorf("fzutil: opening WAV %q: %w", samplePath, err)
	}
	defer fh.Close() //nolint:errcheck
	f, err := wav.Read(fh)
	if err != nil {
		return nil, fmt.Errorf("fzutil: reading WAV %q: %w", samplePath, err)
	}
	return f, nil
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
	res, err := assembleSingle(ctx, regions, wavFiles, targetRate, fitToDisk)
	if err != nil {
		return err
	}
	out := res.FZF
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
//
// Length is counted in frames, not samples. A file that reaches the
// estimate still interleaved would otherwise measure double and step
// the rate down earlier than it needs to.
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
		frames := len(f.Samples)
		if f.Channels > 1 {
			frames /= int(f.Channels)
		}
		ratio := float64(targetRate) / float64(f.SampleRate)
		outSamples := int(math.Round(float64(frames) * ratio))
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

// walkWAVsFS collects every WAV in fsys at any depth, as slash
// separated paths from the root. The paths drive the sort order and
// the voice names. A nested file therefore keeps its folder in the
// order and its base name as the voice name.
func walkWAVsFS(fsys fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(p), ".wav") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sfzconvert: reading directory: %w", err)
	}
	return paths, nil
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
