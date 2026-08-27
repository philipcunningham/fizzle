package webcore

import (
	"bytes"
	"sort"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// Import estimate verdicts and refusal reasons. The verdict names
// what the matching import call would do with the same files, rate,
// and channel; the reason narrows a refusal to the constraint that
// bites first.
const (
	VerdictFits    = "fits"
	VerdictSplits  = "splits"
	VerdictWontFit = "wont-fit"

	ReasonSampleMemory = "sample-memory"
	ReasonDiskRoom     = "disk-room"
	ReasonVoiceLimit   = "voice-limit"
)

// ImportEstimate is the authoritative pre-flight answer for a WAV
// import: what the conversion would add and whether it can land. It
// shares fzutil.ResampledLen with the conversion itself, so the
// figures cannot drift from what an accepted import then produces.
type ImportEstimate struct {
	// Bytes is the document's full dump growth: sector padded audio
	// plus any voice header sectors the new slots need, plus the
	// dump base when the import brings the instrument into being.
	Bytes int `json:"bytes"`
	// Seconds is the batch's play time at the target rate.
	Seconds float64 `json:"seconds"`
	// RoomSeconds is how much more mono audio the document holds at
	// the target rate within its current disk count, bounded by real
	// free sectors and the sampler's sample memory. A larger import
	// may still land by splitting; the verdict reports that.
	RoomSeconds float64 `json:"roomSeconds"`
	Verdict     string  `json:"verdict"`
	// Reason narrows VerdictWontFit; empty otherwise.
	Reason string `json:"reason,omitempty"`
	// AudioAfterBytes is what the instrument would ask the sampler to
	// hold once this import lands, and MemoryBytes is what the user
	// says their sampler has (R27). The dialog states both and infers
	// nothing: neither figure changes what fizzle refuses to build.
	AudioAfterBytes int `json:"audioAfterBytes"`
	MemoryBytes     int `json:"memoryBytes"`
	// AnyStereo is set when at least one file carries two channels,
	// which is what makes the left, right, or mix question worth
	// asking.
	AnyStereo bool `json:"anyStereo"`
	// OverCapFile names the first file over the sampler's memory at
	// the target rate, with its play time in FileSeconds and the
	// longest play time that memory loads at this rate in CapSeconds.
	OverCapFile string  `json:"overCapFile,omitempty"`
	FileSeconds float64 `json:"fileSeconds,omitempty"`
	CapSeconds  float64 `json:"capSeconds,omitempty"`
	// FitsAtRates lists the rates at which the whole batch would be
	// accepted, for a refusal's way out; empty when none would.
	FitsAtRates []int `json:"fitsAtRates,omitempty"`
}

// wavProfile is one parsed file's shape: everything the estimate
// needs without holding on to the samples.
type wavProfile struct {
	name    string
	frames  int
	srcRate uint32
	stereo  bool
}

// docProfile is the open document's shape: the dump the import grows
// and the room around it.
type docProfile struct {
	dumpLen     int  // stitched full dump length; 0 without an instrument
	voiceSlots  int  // header voice slots, placeholder included
	placeholder bool // an empty instrument's slot 0 awaits the first join
	audioBytes  int  // the dump's audio area
	looseFiles  int  // directory entries besides the full dump
	freeSectors int  // unallocated sectors on disk 1
	hasImage    bool
	disks       int // 1, or 2 for a split document
	// memoryBytes is the machine the user declared (R27). It bounds
	// what fizzle reports, never what it refuses: the refusals below
	// keep the hardware's own ceiling, since a disk is not a load and
	// the user may be building for someone else's sampler.
	memoryBytes int
}

// EstimateImport reports what importing files at rate would do to the
// document, without touching it: the size and play time the batch
// becomes, the room left, and whether the import fits, splits the
// instrument across two disks, or is refused. It reads every file
// the way the conversion would, so an unreadable WAV is refused here
// with the same error the import would give.
func (s *Session) EstimateImport(files map[string][]byte, rate uint32, channel string) (*ImportEstimate, *Error) {
	if err := disk.ValidateRate(rate); err != nil {
		return nil, errf("invalid-rate", "%v", err)
	}
	if _, cerr := parseChannel(channel); cerr != nil {
		return nil, cerr
	}
	if len(files) == 0 {
		return nil, errf(codeInvalidValue, "no files to estimate")
	}
	// A lone half of a split pair refuses every mutation, so the
	// estimate refuses too rather than promise a doomed import.
	if cerr := s.checkWholeDocument(); cerr != nil {
		return nil, cerr
	}

	profiles, cerr := profileWAVs(files)
	if cerr != nil {
		return nil, cerr
	}
	doc, cerr := s.profileDocument()
	if cerr != nil {
		return nil, cerr
	}

	est := estimateAt(profiles, doc, rate)
	est.AnyStereo = anyStereo(profiles)
	est.RoomSeconds = roomSeconds(doc, rate)
	if est.Verdict == VerdictWontFit {
		for _, r := range disk.SampleRates {
			if alt := estimateAt(profiles, doc, r); alt.Verdict != VerdictWontFit {
				est.FitsAtRates = append(est.FitsAtRates, int(r))
			}
		}
	}
	return est, nil
}

// anyStereo reports whether the batch holds a file with a left and
// right to choose between.
func anyStereo(profiles []wavProfile) bool {
	for _, p := range profiles {
		if p.stereo {
			return true
		}
	}
	return false
}

// profileWAVs parses each file's header and sample count, in name
// order so refusals land on the same file every time.
func profileWAVs(files map[string][]byte) ([]wavProfile, *Error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]wavProfile, 0, len(names))
	for _, name := range names {
		f, err := wav.Read(bytes.NewReader(files[name]))
		if err != nil {
			return nil, wavRefusal(name, err)
		}
		channels := int(f.Channels)
		if channels < 1 {
			channels = 1
		}
		if f.SampleRate < fzutil.MinSampleRate {
			return nil, errItemf("invalid-wav", name, "%s declares a sample rate fizzle cannot read", name)
		}
		profiles = append(profiles, wavProfile{
			name:    name,
			frames:  len(f.Samples) / channels,
			srcRate: f.SampleRate,
			stereo:  channels >= 2,
		})
	}
	return profiles, nil
}

// profileDocument reads the document's dump and disk shape. A fresh
// or absent disk profiles as empty; the import paths format one on
// demand, so the estimate assumes the same.
func (s *Session) profileDocument() (*docProfile, *Error) {
	doc := &docProfile{disks: 1, freeSectors: disk.SectorCount, memoryBytes: s.sampleMemory()}
	if !s.state.IsOpen() {
		return doc, nil
	}
	doc.hasImage = true
	if s.state.HasSecondDisk() {
		doc.disks = 2
	}
	img, ierr := s.openedImage()
	if ierr != nil {
		return nil, ierr
	}
	doc.freeSectors = img.FreeSectors()
	doc.looseFiles = diskfs.LooseFileCount(img)
	if s.instrument == nil {
		return doc, nil
	}
	fzf, cerr := s.stitchedDump(img)
	if cerr != nil {
		return nil, cerr
	}
	disVN := 0
	if s.state.UsesDIS() {
		disVN = disVoiceCount(img)
	}
	d, cerr := newDumpState(fzf, disVN)
	if cerr != nil {
		return nil, cerr
	}
	doc.dumpLen = len(d.fzf)
	doc.voiceSlots = d.header.NVoice
	doc.placeholder = d.doc.HasPlaceholderVoice()
	doc.audioBytes = len(d.fzf) - d.audioStart
	return doc, nil
}

// estimateAt runs the verdict arithmetic for one target rate,
// mirroring the paths the shell drives: a join onto an instrument and
// a multi file folder conversion re-split as their size dictates,
// while a single first import must fit one disk.
func estimateAt(profiles []wavProfile, doc *docProfile, rate uint32) *ImportEstimate {
	audioSum := 0
	seconds := 0.0
	for _, p := range profiles {
		samples, err := fzutil.ResampledLen(p.frames, p.srcRate, rate)
		if err != nil {
			return &ImportEstimate{
				Verdict:     VerdictWontFit,
				Reason:      ReasonSampleMemory,
				OverCapFile: p.name,
				FileSeconds: float64(p.frames) / float64(p.srcRate),
				CapSeconds:  float64(fzutil.MaxResampleOut) / float64(rate),
			}
		}
		audioSum += sectorPad(samples * disk.BytesPerSample)
		seconds += float64(samples) / float64(rate)
	}

	hasInstrument := doc.dumpLen > 0
	var growth, newLen int
	switch {
	case hasInstrument:
		newSlots := doc.voiceSlots + len(profiles)
		if doc.placeholder {
			newSlots--
		}
		// The format holds 64 voices; every import path refuses the
		// 65th, and no rate changes the count.
		if newSlots > disk.MaxVoices {
			return &ImportEstimate{Verdict: VerdictWontFit, Reason: ReasonVoiceLimit, Seconds: seconds}
		}
		growth = (disk.VoiceAreaSectors(newSlots)-disk.VoiceAreaSectors(doc.voiceSlots))*disk.SectorSize + audioSum
		newLen = doc.dumpLen + growth
	case len(profiles) > disk.MaxVoices:
		return &ImportEstimate{Verdict: VerdictWontFit, Reason: ReasonVoiceLimit, Seconds: seconds}
	default:
		// A fresh instrument: one bank sector plus the voice area the
		// batch needs. Every path re-splits as its size dictates, the
		// lone first voice included.
		newLen = (1+disk.VoiceAreaSectors(len(profiles)))*disk.SectorSize + audioSum
		growth = newLen
	}
	splitCapable := true

	est := &ImportEstimate{
		Bytes:           growth,
		Seconds:         seconds,
		AudioAfterBytes: doc.audioBytes + audioSum,
		MemoryBytes:     doc.memoryBytes,
	}
	switch {
	case newLen <= voicebuild.MaxDiskFileBytes && fitsFreeSectors(doc, newLen):
		est.Verdict = VerdictFits
	case splitCapable && doc.looseFiles == 0 &&
		newLen <= 2*voicebuild.MaxDiskFileBytes &&
		doc.audioBytes+audioSum <= disk.MaxSampleRAM:
		est.Verdict = VerdictSplits
	default:
		est.Verdict = VerdictWontFit
		est.Reason = ReasonDiskRoom
	}
	return est
}

// fitsFreeSectors checks the one disk case against the image's real
// free space: the rewritten dump and its DIS sector have to land in
// what the old dump gives back plus what is free.
func fitsFreeSectors(doc *docProfile, newLen int) bool {
	if !doc.hasImage {
		return true
	}
	need := sectorPad(newLen)/disk.SectorSize + 1
	avail := doc.freeSectors
	if doc.dumpLen > 0 {
		avail += sectorPad(doc.dumpLen)/disk.SectorSize + 1
	}
	return need <= avail
}

// roomSeconds converts the document's remaining capacity to play
// time at the target rate: the tightest of the dump maximum, the
// disk's real free sectors (loose files eat them), and the sampler's
// sample memory. The figure covers the document's current disk
// count; a larger import may still land by splitting, which the
// verdict reports separately.
func roomSeconds(doc *docProfile, rate uint32) float64 {
	room := min(
		doc.disks*voicebuild.MaxDiskFileBytes-doc.dumpLen,
		doc.memoryBytes-doc.audioBytes,
	)
	if doc.hasImage && doc.disks == 1 {
		// The same arithmetic fitsFreeSectors applies: the rewritten
		// dump lands in what the old one gives back plus what is free.
		oldSectors := 0
		if doc.dumpLen > 0 {
			oldSectors = sectorPad(doc.dumpLen) / disk.SectorSize
		}
		room = min(room, (doc.freeSectors+oldSectors)*disk.SectorSize-doc.dumpLen)
	}
	if room < 0 {
		room = 0
	}
	return float64(room) / disk.BytesPerSample / float64(rate)
}

// sectorPad rounds n up to a whole sector, the granularity every
// dump area grows in.
func sectorPad(n int) int {
	return (n + disk.SectorSize - 1) / disk.SectorSize * disk.SectorSize
}
