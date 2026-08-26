package webcore

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	documentmodel "github.com/philipcunningham/fizzle/pkg/document"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// OpenImagePair opens a two disk split instrument as one document
// (R5). The images may arrive in either order: the directory entry's
// disk number byte says which is which.
func (s *Session) OpenImagePair(a, b []byte) (Snapshot, *Error) {
	for i, data := range [][]byte{a, b} {
		if len(data) != disk.ImageSize {
			return s.Snapshot(), errf("invalid-image", "image %d: an FZ image is %d bytes, got %d", i+1, disk.ImageSize, len(data))
		}
	}
	imgA, errA := disk.ReadImage(bytes.NewReader(a))
	if errA != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", errA)
	}
	imgB, errB := disk.ReadImage(bytes.NewReader(b))
	if errB != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", errB)
	}

	numA, okA := dumpDiskNum(imgA)
	numB, okB := dumpDiskNum(imgB)
	if !okA || !okB {
		return s.Snapshot(), errf(codePairMismatch, "both images must carry a %s entry", disk.FullDumpName)
	}
	img1, img2 := imgA, imgB
	switch {
	case numA == 1 && numB == 0:
		img1, img2 = imgB, imgA
	case numA == 0 && numB == 1:
		// already in order
	default:
		return s.Snapshot(), errf(codePairMismatch, "the images are disks %d and %d of a set; a pair is disks 1 and 2", numA+1, numB+1)
	}

	// The stitched dump must parse as one instrument; a mismatched pair
	// fails here rather than corrupting later edits.
	fzf, gerr := diskfs.Extract(img1, disk.FullDumpName)
	if gerr != nil {
		return s.Snapshot(), errf(codePairMismatch, "%v", gerr)
	}
	// Disk 1 has to want a continuation before one is stitched on. A
	// complete one disk dump answers for all of its own audio, so
	// appending anything to it lengthens a whole file: ExtractFile then
	// hands back 503,808 bytes where the disk's dump is 9,216. This is
	// the test missingDiskOf applies to a lone image, so what the shell
	// offers a pair for and what a pair is accepted for agree.
	if !needsContinuation(fzf) {
		return s.Snapshot(), errf(codePairMismatch,
			"disk 1 holds a complete instrument and needs no continuation, so these are not disks 1 and 2 of one set; open it on its own")
	}
	cont, gerr := diskfs.Extract(img2, disk.FullDumpName)
	if gerr != nil {
		return s.Snapshot(), errf(codePairMismatch, "%v", gerr)
	}
	stitched := append(append([]byte{}, fzf...), cont...)
	hdr, herr := fzutil.ParseFZFHeader(stitched)
	if herr != nil {
		return s.Snapshot(), errf(codePairMismatch, "the stitched dump does not parse: %v", herr)
	}
	if cerr := checkContinuation(stitched, hdr); cerr != nil {
		return s.Snapshot(), cerr
	}

	// Opening a pair is opening a document: history starts fresh, so
	// undo cannot reach back past it into a disk the user has left.
	return s.adoptFresh(img1, img2.Bytes())
}

// needsContinuation reports whether disk 1's own bytes say its audio is
// only part of the instrument's. Two things can say so, and either is
// enough: the bank sector's total wave marker, which isSplitDisk1 reads
// and corroborates against the voice headers (the same test
// missingDiskOf applies), or a voice whose samples run past the audio
// this disk holds, which is what a split falling mid-voice leaves on an
// image the FZ-1 wrote without a marker.
//
// A dump that says neither is whole, and stitching anything onto it
// makes a longer file rather than a bigger instrument.
func needsContinuation(fzf []byte) bool {
	if isSplitDisk1(fzf) {
		return true
	}
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		return false
	}
	voiceAreaEnd := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if len(fzf) < voiceAreaEnd || voiceAreaEnd < disk.SectorSize {
		return false
	}
	return checkCoversLastVoice(fzf, hdr, voiceAreaEnd) != nil
}

// checkContinuation verifies that disk 2 carries the audio disk 1 says
// is missing. Disk 1's bank sector stamps the instrument's total wave
// sector count, and its last voice's wave end says how far the audio
// runs, so a continuation from a different instrument shows up as the
// wrong length. Without this, two disks from unrelated split
// instruments stitch silently and every voice past the boundary plays
// another instrument's samples.
func checkContinuation(stitched []byte, hdr *fzutil.FZFHeader) *Error {
	voiceAreaEnd := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if len(stitched) < voiceAreaEnd || voiceAreaEnd < disk.SectorSize {
		return errf(codePairMismatch, "the stitched dump is shorter than its own header")
	}
	// The furthest voice's wave end says where the audio has to reach.
	// This holds whether or not disk 1 stamped a total marker, so it is
	// the check that catches a short continuation on the images the
	// FZ-1 wrote without one.
	if cerr := checkCoversLastVoice(stitched, hdr, voiceAreaEnd); cerr != nil {
		return cerr
	}

	wantSectors := int(binary.LittleEndian.Uint32(stitched[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	if wantSectors <= 0 {
		// Disk 1 carries no total marker (the FZ-1 does not always
		// write it). The wave-end check above is all this image
		// supports: a stranger's disk 2 that happens to be long enough
		// is indistinguishable from the real one in these bytes.
		return nil
	}
	// The stitched audio must be the size disk 1 declared. A sector of
	// slack absorbs the tail rounding a stored file can carry.
	gotSectors := (len(stitched) - voiceAreaEnd) / disk.SectorSize
	if gotSectors < wantSectors || gotSectors > wantSectors+1 {
		return errf(codePairMismatch,
			"disk 2 carries %d audio sectors; this instrument needs %d, so the disks are not a pair",
			gotSectors, wantSectors)
	}
	return nil
}

// checkCoversLastVoice verifies the stitched audio reaches the furthest
// wave end any voice slot points at. A voice whose samples run past the
// end of the audio would play whatever follows, or nothing; a disk 2
// from another instrument is usually too short for this instrument's
// last voice.
func checkCoversLastVoice(stitched []byte, hdr *fzutil.FZFHeader, voiceAreaEnd int) *Error {
	voiceArea := stitched[hdr.VoiceAreaStart:]
	maxEnd := 0
	for slot := 0; slot < hdr.NVoice; slot++ {
		off := disk.VoiceSlotOffset(0, slot)
		if off+disk.VoiceHeaderUsed > len(voiceArea) {
			break
		}
		end := int(binary.LittleEndian.Uint32(voiceArea[off+disk.VoiceWaveEndOffset:]))
		if end > maxEnd {
			maxEnd = end
		}
	}
	need := maxEnd * disk.BytesPerSample
	got := len(stitched) - voiceAreaEnd
	if got < need {
		return errf(codePairMismatch,
			"the stitched audio holds %d bytes; this instrument's voices run to %d, so the disks are not a pair",
			got, need)
	}
	return nil
}

func (s *Session) stitchedDump(img *disk.Image) ([]byte, *Error) {
	return stitchedDumpPair(img, s.state.Image2())
}

// stitchedDumpPair returns a candidate document's FULL-DATA-FZ payload. For a
// split document it appends disk 2's audio continuation, the same stitch the
// CLI's voice unpack performs on a pair.
func stitchedDumpPair(img *disk.Image, image2 []byte) ([]byte, *Error) {
	fzf, gerr := diskfs.Extract(img, disk.FullDumpName)
	if gerr != nil {
		return nil, errf(codeNotFound, "%v", gerr)
	}
	if image2 == nil {
		return fzf, nil
	}
	img2, rerr := disk.ReadImage(bytes.NewReader(image2))
	if rerr != nil {
		return nil, errf("invalid-image", "disk 2 unreadable: %v", rerr)
	}
	cont, gerr := diskfs.Extract(img2, disk.FullDumpName)
	if gerr != nil {
		return nil, errf(codeNotFound, "disk 2: %v", gerr)
	}
	return append(fzf, cont...), nil
}

// replaceDump writes a full dump back into the document, splitting
// across two images when it outgrows one disk and collapsing back to
// one when it no longer does. img is the parsed disk 1 scratch image;
// on error the session state is untouched. vn is the voice count the
// DIS tail must carry, 0 to let content detection derive it.
func (s *Session) replaceDump(img *disk.Image, fzf []byte, vn int, mode parseMode) (Snapshot, *Error) {
	usesDIS := s.state.UsesDIS()
	if mode == modeDerive {
		usesDIS = documentDISMode(img)
	}
	working, err := documentmodel.NewState(img.Bytes(), s.state.Image2(), authorityFromDIS(usesDIS))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "%v", err)
	}
	next, err := working.ReplaceFullDump(fzf, vn, authorityFromDIS(usesDIS))
	if err != nil {
		return s.Snapshot(), documentMutationError(err)
	}
	nextImage, err := disk.ReadImage(bytes.NewReader(next.Image1()))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "%v", err)
	}
	return s.adoptPair(nextImage, next.Image2(), mode)
}

// looseFileCount counts directory entries other than the full dump.
func looseFileCount(img *disk.Image) int {
	entries, err := img.Directory()
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.NameString() != disk.FullDumpName {
			n++
		}
	}
	return n
}

func documentMutationError(err error) *Error {
	var occupied *documentmodel.ErrSplitNeedsEmptyDisk
	if errors.As(err, &occupied) {
		return errf("no-space", "a two disk instrument fills disk 1 completely; extract the disk's %d other files first", occupied.Files)
	}
	if errors.Is(err, disk.ErrNoSpace) {
		return addError(err)
	}
	return splitError(err)
}

// splitError maps the split's failure modes onto boundary codes (R10:
// what cannot fit a two disk set is rejected with the core's error).
func splitError(err error) *Error {
	var tmd *voicebuild.ErrTooManyDisks
	if errors.As(err, &tmd) {
		return errf("too-large", "%v", tmd)
	}
	var ram *voicebuild.ErrSampleRAMExceeded
	if errors.As(err, &ram) {
		return errf("ram-exceeded", "%v", ram)
	}
	return errf(codeInvalidValue, "%v", err)
}

// dumpDiskNum reads the disk number byte of an image's FULL-DATA-FZ
// entry: 0 for disk 1 (or a one disk dump), 1 for the continuation.
func dumpDiskNum(img *disk.Image) (int, bool) {
	entries, err := img.Directory()
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		if e.NameString() == disk.FullDumpName {
			return int(e.DiskNum), true
		}
	}
	return 0, false
}

// missingDiskOf reports which half of a pair is absent when a lone
// image belongs to a split set: 2 when this is disk 1 of two, 1 when
// this is the continuation disk, 0 for a whole document.
func missingDiskOf(img *disk.Image, img2 []byte) int {
	if img2 != nil {
		return 0
	}
	num, ok := dumpDiskNum(img)
	if !ok {
		return 0
	}
	if num == 1 {
		return 1
	}
	fzf, err := diskfs.Extract(img, disk.FullDumpName)
	if err == nil && isSplitDisk1(fzf) {
		return 2
	}
	return 0
}

// isSplitDisk1 detects disk 1 of a two disk dump the way fzfinfo does:
// the bank sector's total wave marker exceeds the local audio, and a
// plausible boundary voice's wave start points past it. The FZ-1 does
// not always write the marker, so the corroboration guards against
// garbage.
func isSplitDisk1(fzf []byte) bool {
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		return false
	}
	voiceAreaEnd := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if len(fzf) < voiceAreaEnd || voiceAreaEnd < disk.SectorSize {
		return false
	}
	totalWaveMarker := int(binary.LittleEndian.Uint32(fzf[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	localAudioBytes := len(fzf) - voiceAreaEnd
	localWaveSectors := localAudioBytes / disk.SectorSize
	if totalWaveMarker <= 0 || totalWaveMarker <= localWaveSectors {
		return false
	}
	// Corroborate with the voice headers: a voice whose audio starts
	// past the local area (fzfinfo's boundary test), or one whose audio
	// ends past it (a split that falls mid-voice, which fzfinfo's
	// start-only test misses on dumps of few large voices).
	voiceArea := fzf[hdr.VoiceAreaStart:voiceAreaEnd]
	for i := 0; i < hdr.NVoice; i++ {
		off := disk.VoiceSlotOffset(0, i)
		if off+disk.VoiceHeaderUsed > len(voiceArea) {
			break
		}
		slot := voiceArea[off : off+disk.VoiceHeaderUsed]
		if !disk.IsPlausibleVoiceSlot(slot) {
			continue
		}
		wavst := binary.LittleEndian.Uint32(slot[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
		waved := binary.LittleEndian.Uint32(slot[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])
		if int(wavst)*disk.BytesPerSample >= localAudioBytes || int(waved)*disk.BytesPerSample > localAudioBytes {
			return true
		}
	}
	return false
}
