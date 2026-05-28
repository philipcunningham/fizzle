package voiceedit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
)

var playbackModeNames = [4]string{"normal", "reverse", "cue", "synth"}

func FuzzFZVEditChain(f *testing.F) {
	f.Add([]byte{0, 50, 1, 100, 2, 3})
	f.Add([]byte{12, 0, 13, 60, 14, 96, 15, 72})
	f.Add([]byte{18, 65, 66, 67, 19, 2})
	f.Add([]byte{0, 127, 1, 127, 2, 5, 3, 127, 4, 127, 5, 127, 6, 127, 7, 127, 8, 7, 9, 7, 10, 7, 11, 7})

	f.Fuzz(func(t *testing.T, seed []byte) {
		if len(seed) == 0 {
			return
		}

		data := testutil.MakeTestVoice("FUZZ", 1000)
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.fzv")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		origSize := int64(len(data))

		limit := len(seed)
		if limit > 200 {
			limit = 200
		}

		ops := 0
		for i := 0; i < limit && ops < 100; i++ {
			cat := seed[i] % 20
			i++
			var valByte byte
			if i < limit {
				valByte = seed[i]
			} else {
				valByte = seed[0]
			}

			var patches []Patch
			var err error

			switch cat {
			case 0:
				patches, err = BuildFilterPatches(int(valByte%128), Unchanged)
			case 1:
				patches, err = BuildFilterPatches(Unchanged, int(valByte%128))
			case 2:
				patches, err = BuildLFOPatches(int(valByte%6), Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
			case 3:
				patches, err = BuildLFOPatches(Unchanged, int(valByte%128), Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
			case 4:
				patches, err = BuildLFOPatches(Unchanged, Unchanged, int(valByte%128), Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
			case 5:
				patches, err = BuildLFOPatches(Unchanged, Unchanged, Unchanged, Unchanged, int(valByte%128), Unchanged, Unchanged, Unchanged, 0)
			case 6:
				patches, err = BuildLFOPatches(Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, int(valByte%128), Unchanged, Unchanged, 0)
			case 7:
				patches, err = BuildLFOPatches(Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, int(valByte%128), Unchanged, 0)
			case 8:
				patches, err = BuildDCAPatches(int(valByte%8), Unchanged,
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]uint8{})
			case 9:
				patches, err = BuildDCAPatches(Unchanged, int(valByte%8),
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]uint8{})
			case 10:
				patches, err = BuildDCFPatches(int(valByte%8), Unchanged,
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]uint8{})
			case 11:
				patches, err = BuildDCFPatches(Unchanged, int(valByte%8),
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
					[disk.EnvelopeStages]uint8{})
			case 12:
				tuneVal := int(int8(valByte)) * 128 //nolint:gosec // fuzz test: intentional signed reinterpretation for range mapping
				patches, err = BuildTunePatch(tuneVal)
			case 13:
				patches, err = BuildKeyRangePatch(int(valByte%128), Unchanged, Unchanged)
			case 14:
				patches, err = BuildKeyRangePatch(Unchanged, int(valByte%128), Unchanged)
			case 15:
				patches, err = BuildKeyRangePatch(Unchanged, Unchanged, int(valByte%128))
			case 16:
				patches, err = BuildModulationPatches(Unchanged, Unchanged, Unchanged, Unchanged, int(valByte%128), Unchanged, Unchanged, Unchanged, Unchanged)
			case 17:
				patches, err = BuildModulationPatches(Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, int(valByte%128), Unchanged, Unchanged, Unchanged)
			case 18:
				nameLen := int(valByte%disk.LabelSize) + 1
				nameBytes := make([]byte, nameLen)
				for j := range nameBytes {
					idx := i + 1 + j
					var ch byte
					if idx < limit {
						ch = seed[idx]
					} else {
						ch = seed[j%len(seed)]
					}
					nameBytes[j] = 'A' + ch%26
				}
				patches, err = BuildNamePatch(string(nameBytes))
			case 19:
				patches, err = BuildPlaybackModePatch(playbackModeNames[valByte%4])
			}

			if err != nil {
				ops++
				continue
			}
			if len(patches) == 0 {
				ops++
				continue
			}

			if err := ApplyToFZV(path, patches); err != nil {
				t.Fatalf("ApplyToFZV failed on op %d (cat=%d): %v", ops, cat, err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat after op %d: %v", ops, err)
			}
			if info.Size() != origSize {
				t.Fatalf("file size changed after op %d: %d != %d", ops, info.Size(), origSize)
			}

			if _, err := fzvinfo.Parse(path); err != nil {
				t.Fatalf("fzvinfo.Parse failed after op %d (cat=%d): %v", ops, cat, err)
			}

			ops++
		}

		finalData, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := voiceextract.Decode(finalData); err != nil {
			t.Fatalf("voiceextract.Decode failed after %d ops: %v", ops, err)
		}
	})
}
