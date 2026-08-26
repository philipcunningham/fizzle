package webcore

import (
	"bytes"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// The two sweeps below cover both dump shapes. Instruments this package
// assembles have summed bstep values equal to the walked voice count.
// Real dumps mostly do not: areas share voices through vp[], the sum
// runs above the count, and the walk ends on the audio's own bytes.
// Every operation has to hold on both, so one sweep runs each operation
// over every dump in testdata and the other walks random sequences.

// areaOp names one operation and the patch builder that performs it.
type areaOp struct {
	name  string
	build func(d *dumpState) ([]model.Patch, *Error)
}

// areaOpsFor returns the operations to try against one dump, aimed at
// the indices that dump actually holds.
func areaOpsFor(t *testing.T, fzf []byte, hdr *fzutil.FZFHeader) []areaOp {
	t.Helper()
	lastArea := bankBstep(fzf, 0) - 1
	oneArea := fzbPlaying(t, 0)
	return []areaOp{
		{"AddArea", func(d *dumpState) ([]model.Patch, *Error) { return addAreaPatches(d, 0, 0) }},
		{"DeleteArea first", func(d *dumpState) ([]model.Patch, *Error) { return deleteAreaPatches(d, 0, 0) }},
		{"DeleteArea last", func(d *dumpState) ([]model.Patch, *Error) { return deleteAreaPatches(d, 0, lastArea) }},
		{"DuplicateArea", func(d *dumpState) ([]model.Patch, *Error) { return duplicateAreaPatches(d, 0, 0) }},
		{"MapVoice", func(d *dumpState) ([]model.Patch, *Error) { return mapVoicePatches(d, 0) }},
		{"AddBank append", func(d *dumpState) ([]model.Patch, *Error) {
			return addBankDocumentOperation(d, oneArea, hdr.NBankSectors)
		}},
		{"AddBank replace", func(d *dumpState) ([]model.Patch, *Error) { return addBankDocumentOperation(d, oneArea, 0) }},
	}
}

// resolvedVN is the count a dump's geometry reads under: the resolved
// explicit count, or 0 where the walk decides.
func resolvedVN(data []byte, candidate int) int {
	layout, err := fzutil.ResolveDiskFZFLayout(data, candidate)
	if err != nil || layout.VoiceCountSource() == fzutil.VoiceCountWalk {
		return 0
	}
	return layout.VoiceCount()
}

// sweptDump is one corpus dump plus its DIS-mode voice count, 0 for
// walk mode.
type sweptDump struct {
	data []byte
	vn   int
}

// corpusDumps collects every full dump under testdata: the standalone
// .FZF files and the FULL-DATA-FZ payload of every .img.
func corpusDumps(t *testing.T) map[string]sweptDump {
	t.Helper()
	root := filepath.Join("..", "..", "testdata")
	var paths []string
	err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // a walk error fails the test as it is
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !de.IsDir() && (ext == ".fzf" || ext == ".img") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk testdata: %v", err)
	}
	out := map[string]sweptDump{}
	for _, p := range paths {
		data, rerr := os.ReadFile(p) // #nosec G304 -- test fixtures under testdata
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		vn := 0
		if strings.ToLower(filepath.Ext(p)) == ".img" {
			// A fixture that is not a readable image, or that holds no
			// full dump, is simply not a dump to sweep.
			img, ierr := disk.ReadImage(bytes.NewReader(data))
			if ierr != nil {
				continue
			}
			fzf, gerr := diskget.FromImage(img, disk.FullDumpName)
			if gerr != nil {
				continue
			}
			data = fzf
			vn = resolvedVN(data, disVoiceCount(img))
		}
		if _, perr := fzutil.ParseFZFHeader(data); perr == nil {
			out[p] = sweptDump{data: data, vn: vn}
		}
	}
	if len(out) < 100 {
		t.Fatalf("found %d dumps under testdata, expected the corpus", len(out))
	}
	return out
}

// assertAudioHeld checks the one thing no area operation may do: move
// the audio, or change the samples a voice plays. A refusal is a fine
// answer to an operation the format cannot express; a wrong dump is
// not.
func assertAudioHeld(t *testing.T, what string, before, after dumpGeometry) {
	t.Helper()
	if before.bstepSum == before.walked && after.bstepSum != after.walked {
		t.Errorf("%s: summed bstep = %d but the walk yields %d voices: the walk now runs past the last slot",
			what, after.bstepSum, after.walked)
	}
	if !bytes.Equal(before.audio, after.audio) {
		t.Errorf("%s: the audio area moved: it started at %d holding %d bytes, now starts at %d holding %d",
			what, before.audioStart, len(before.audio), after.audioStart, len(after.audio))
		return
	}
	for name, want := range before.voices {
		got, ok := after.voices[name]
		if !ok {
			continue // a deleted area may take its voice with it
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s: voice %q plays different audio: %d bytes then %d", what, name, len(want), len(got))
		}
	}
	for extent := range after.extents {
		if !before.extents[extent] {
			t.Errorf("%s: a voice now plays samples %d to %d, which no voice played before",
				what, extent[0], extent[1])
		}
	}
}

// Every area operation, over every dump under testdata. A sweep of this
// shape found one dump of 224 damaged by duplicate, which no other test
// in the tree catches.
func TestAreaOpsOverTheCorpus(t *testing.T) {
	dumps := corpusDumps(t)
	shared, tried, refused := 0, 0, 0
	refusedBy := map[string]int{}
	for path, d := range dumps {
		fzf := d.data
		hdr := resolvedHeaderFor(t, fzf, d.vn)
		before := dumpGeometryUnder(t, fzf, d.vn)
		if before.bstepSum > before.walked {
			shared++
		}
		for _, op := range areaOpsFor(t, fzf, hdr) {
			tried++
			out, outVN, cerr := patchDumpBytes(bytes.Clone(fzf), d.vn, op.build)
			if cerr != nil {
				refused++
				refusedBy[op.name+": "+cerr.Code]++
				continue
			}
			after := dumpGeometryUnder(t, out, resolvedVN(out, outVN))
			assertAudioHeld(t, fmt.Sprintf("%s on %s", op.name, filepath.Base(path)),
				before, after)
		}
	}
	t.Logf("%d dumps (%d sharing voices through vp[]), %d operations, %d refused",
		len(dumps), shared, tried, refused)
	for what, n := range refusedBy {
		t.Logf("  refused %3d %s", n, what)
	}
}

// The join path writes a voice header at the walked count as well, and
// lands the incoming PCM at the end of the audio area, so it gets the
// same sweep. The audio it inherits has to survive as a prefix of the
// audio it leaves.
func TestAddVoiceOverTheCorpus(t *testing.T) {
	voice := testFZV(t, "JOINED", 1500)
	dumps := corpusDumps(t)
	refusedBy := map[string]int{}
	for path, dump := range dumps {
		fzf := dump.data
		before := dumpGeometryUnder(t, fzf, dump.vn)
		out, outVN, cerr := patchDumpBytes(bytes.Clone(fzf), dump.vn, func(d *dumpState) ([]model.Patch, *Error) {
			result, err := d.doc.AddVoice(voice)
			if err != nil {
				return nil, addVoiceDocumentError(err)
			}
			return nil, applyDocumentOperation(d, result)
		})
		if cerr != nil {
			refusedBy[cerr.Code]++
			continue
		}
		after := dumpGeometryUnder(t, out, resolvedVN(out, outVN))
		what := "AddVoice on " + filepath.Base(path)
		if len(after.audio) < len(before.audio) || !bytes.Equal(before.audio, after.audio[:len(before.audio)]) {
			t.Errorf("%s: the audio already on the disk moved: it started at %d holding %d bytes, now starts at %d holding %d",
				what, before.audioStart, len(before.audio), after.audioStart, len(after.audio))
			continue
		}
		// The joined voice brings a range of its own, so the check is
		// that every range the dump already declared is still declared.
		for extent := range before.extents {
			if !after.extents[extent] {
				t.Errorf("%s: no voice plays samples %d to %d any more", what, extent[0], extent[1])
			}
		}
	}
	t.Logf("%d dumps joined a voice, refusals by code: %v", len(dumps), refusedBy)
}

// CASIO066 carries bank pointers beyond the count its voice walk resolves.
// Before AddVoice became one document operation, the join reported success
// but left the new slot outside that walk and therefore unreachable. Pin the
// intentional correction: the joined voice must advance the resolved count
// and an area must reference its slot.
func TestAddVoiceMakesJoinedSlotReachableOnCASIO066(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "corpus",
		"casio-fz-1-shareware-library-fzf-format", "CASIO066.FZF")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed test fixture path
	if err != nil {
		t.Fatal(err)
	}
	before := dumpGeometryUnder(t, data, 0)
	voice := testFZV(t, "JOINED", 1500)
	out, outVN, cerr := patchDumpBytes(bytes.Clone(data), 0, func(d *dumpState) ([]model.Patch, *Error) {
		result, addErr := d.doc.AddVoice(voice)
		if addErr != nil {
			return nil, addVoiceDocumentError(addErr)
		}
		return nil, applyDocumentOperation(d, result)
	})
	if cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	after := dumpGeometryUnder(t, out, resolvedVN(out, outVN))
	if after.walked != before.walked+1 {
		t.Fatalf("resolved voice count = %d, want %d", after.walked, before.walked+1)
	}
	hdr := resolvedHeaderFor(t, out, outVN)
	if sites := fzutil.FindBankSitesForVoice(out, hdr, before.walked); len(sites) == 0 {
		t.Fatalf("joined voice slot %d is not referenced by an area", before.walked)
	}
}

// A random walk of area operations, checking after every step that the
// audio has not moved since the sequence began. Single operations on a
// fresh instrument are the easy case; the damage comes from the states
// a sequence reaches, where the count has already crossed a sector
// boundary or the summed bsteps have already parted from the walk.
func TestAreaOpSequencesHoldTheAudio(t *testing.T) {
	const steps = 250
	applied, refusedBy := 0, map[string]int{}
	for _, voices := range []int{1, 2, 4, 5, 8, 17} {
		for seed := uint64(1); seed <= 15; seed++ {
			t.Run(fmt.Sprintf("%d voices seed %d", voices, seed), func(t *testing.T) {
				fzf, _ := nVoiceDump(t, voices)
				origin := dumpGeometryOf(t, fzf)
				rng := rand.New(rand.NewPCG(seed, uint64(voices))) //nolint:gosec // G404 and G115: a reproducible walk over a small positive count, not cryptography
				for step := range steps {
					hdr, err := fzutil.ParseFZFHeader(fzf)
					if err != nil {
						t.Fatalf("step %d: %v", step, err)
					}
					op := randomAreaOp(t, fzf, hdr, rng)
					out, _, cerr := patchDumpBytes(bytes.Clone(fzf), 0, op.build)
					if cerr != nil {
						refusedBy[cerr.Code]++
						continue
					}
					applied++
					fzf = out
					assertAudioHeld(t, fmt.Sprintf("step %d, %s", step, op.name), origin, dumpGeometryOf(t, fzf))
					if t.Failed() {
						return
					}
				}
			})
		}
	}
	t.Logf("%d operations applied, refusals by code: %v", applied, refusedBy)
}

// randomAreaOp picks one operation, aimed at indices the dump holds.
func randomAreaOp(t *testing.T, fzf []byte, hdr *fzutil.FZFHeader, rng *rand.Rand) areaOp {
	t.Helper()
	bank := rng.IntN(hdr.NBankSectors)
	area := rng.IntN(max(bankBstep(fzf, bank), 1))
	slot := rng.IntN(hdr.NVoice)
	switch rng.IntN(5) {
	case 0:
		return areaOp{fmt.Sprintf("AddArea(%d, %d)", bank, slot),
			func(d *dumpState) ([]model.Patch, *Error) { return addAreaPatches(d, bank, slot) }}
	case 1:
		return areaOp{fmt.Sprintf("DeleteArea(%d, %d)", bank, area),
			func(d *dumpState) ([]model.Patch, *Error) { return deleteAreaPatches(d, bank, area) }}
	case 2:
		return areaOp{fmt.Sprintf("DuplicateArea(%d, %d)", bank, area),
			func(d *dumpState) ([]model.Patch, *Error) { return duplicateAreaPatches(d, bank, area) }}
	case 3:
		return areaOp{fmt.Sprintf("MapVoice(%d)", slot),
			func(d *dumpState) ([]model.Patch, *Error) { return mapVoicePatches(d, slot) }}
	default:
		at := rng.IntN(hdr.NBankSectors + 1)
		fzb := fzbPlaying(t, rng.IntN(hdr.NVoice))
		return areaOp{fmt.Sprintf("AddBank(%d)", at),
			func(d *dumpState) ([]model.Patch, *Error) { return addBankDocumentOperation(d, fzb, at) }}
	}
}

// The random walk again in DIS mode, over the bankless dump, tracking
// the count each step stamps. This is the harness for the class of
// defect where a mode decision drifts mid-sequence.
func TestAreaOpSequencesHoldTheAudioInDISMode(t *testing.T) {
	const steps = 250
	applied, refusedBy := 0, map[string]int{}
	for seed := uint64(1); seed <= 15; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			fzf := fzfbuilder.MakeBanklessVoiceDump(t)
			vn := fzfbuilder.BanklessDumpVoices
			origin := dumpGeometryUnder(t, fzf, vn)
			rng := rand.New(rand.NewPCG(seed, 99)) //nolint:gosec // G404: a reproducible walk, not cryptography
			// The count tracks what the write-back stamps, the way the
			// session's sticky mode does.
			for step := range steps {
				hdr := resolvedHeaderFor(t, fzf, vn)
				op := randomAreaOp(t, fzf, hdr, rng)
				out, outVN, cerr := patchDumpBytes(bytes.Clone(fzf), vn, op.build)
				if cerr != nil {
					refusedBy[cerr.Code]++
					continue
				}
				applied++
				fzf, vn = out, outVN
				assertAudioHeld(t, fmt.Sprintf("step %d, %s", step, op.name),
					origin, dumpGeometryUnder(t, fzf, vn))
				if t.Failed() {
					return
				}
			}
		})
	}
	t.Logf("%d operations applied, refusals by code: %v", applied, refusedBy)
}
