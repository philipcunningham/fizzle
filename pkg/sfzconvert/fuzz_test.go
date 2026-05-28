package sfzconvert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/sfz"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func FuzzConvertVoices(f *testing.F) {
	f.Add(uint8(36), uint8(48), uint8(42), int16(100), int16(-100), uint16(100))
	f.Add(uint8(0), uint8(127), uint8(60), int16(0), int16(0), uint16(1))
	f.Add(uint8(60), uint8(60), uint8(60), int16(32767), int16(-32768), uint16(500))
	f.Fuzz(func(t *testing.T, lokey, hikey, root uint8, s1, s2 int16, nSamples uint16) {
		if lokey > hikey {
			return
		}
		if nSamples == 0 || nSamples > 5000 {
			return
		}
		samples := make([]int16, nSamples)
		for i := range samples {
			if i%2 == 0 {
				samples[i] = s1
			} else {
				samples[i] = s2
			}
		}
		r := sfz.NewRegion()
		r.Sample = "test.wav"
		r.LoKey = lokey
		r.HiKey = hikey
		r.PitchKeycenter = root
		regions := []sfz.Region{r}
		wavFiles := map[string]*wav.File{
			"test.wav": {SampleRate: 36000, Samples: samples, LoopStart: -1, LoopEnd: -1},
		}
		voices, keygroups, err := convertVoices(context.Background(), regions, wavFiles, 0, 36000)
		if err != nil {
			t.Fatalf("convertVoices(lokey=%d, hikey=%d, root=%d, s1=%d, s2=%d, nSamples=%d): %v",
				lokey, hikey, root, s1, s2, nSamples, err)
		}
		if len(voices) != 1 {
			t.Fatalf("len(voices) = %d, want 1", len(voices))
		}
		if len(voices[0]) == 0 {
			t.Fatalf("voices[0] is empty")
		}
		if len(keygroups) != 1 {
			t.Fatalf("len(keygroups) = %d, want 1", len(keygroups))
		}
		if keygroups[0].KeyLow != lokey {
			t.Fatalf("keygroups[0].KeyLow = %d, want %d", keygroups[0].KeyLow, lokey)
		}
		if keygroups[0].KeyHigh != hikey {
			t.Fatalf("keygroups[0].KeyHigh = %d, want %d", keygroups[0].KeyHigh, hikey)
		}
		if keygroups[0].KeyCentre != root {
			t.Fatalf("keygroups[0].KeyCentre = %d, want %d", keygroups[0].KeyCentre, root)
		}
	})
}

func FuzzSFZConvertChaos(f *testing.F) {
	f.Add([]byte{0, 60, 5, 10, 50, 30, 2, 0})
	f.Add([]byte{1, 70, 3, 20, 100, 60, 1, 1, 80, 4, 30, 50, 40, 0, 0})
	f.Add([]byte{3, 40, 0, 0, 127, 127, 4, 1, 50, 12, 99, 0, 0, 0, 60, 6, 50, 80, 80, 3, 0, 55, 1, 1, 1, 1, 1, 1})
	f.Add([]byte{2, 90, 10, 60, 0, 0, 0, 0, 45, 7, 40, 64, 64, 2, 1})
	f.Fuzz(func(t *testing.T, seed []byte) {
		if len(seed) < 8 {
			return
		}

		nRegions := int(seed[0])%4 + 1
		seed = seed[1:]

		nextByte := func() byte {
			if len(seed) == 0 {
				return 0
			}
			b := seed[0]
			seed = seed[1:]
			return b
		}

		dir := t.TempDir()

		type regionMeta struct {
			cutoff    int
			resonance int
		}
		metas := make([]regionMeta, nRegions)
		var sfzContent string

		for i := range nRegions {
			key := uint8(36 + i)
			transpose := int8(nextByte()) % 13  //nolint:gosec // G115: intentional wrap for fuzz input
			tune := int(int8(nextByte())) % 101 //nolint:gosec // G115: intentional wrap for fuzz input
			cutoff := int(nextByte())%129 - 1
			resonance := int(nextByte())%129 - 1
			mutegroup := int(nextByte())%5 - 1
			oneShot := nextByte()%2 == 1

			metas[i] = regionMeta{cutoff: cutoff, resonance: resonance}

			wavName := fmt.Sprintf("sample%d.wav", i)
			wavPath := filepath.Join(dir, wavName)
			testutil.WriteTestWAV(t, wavPath, 36000, 100)

			sfzContent += "<region>\n"
			sfzContent += fmt.Sprintf("sample=%s\n", wavName)
			sfzContent += fmt.Sprintf("lokey=%d hikey=%d pitch_keycenter=%d\n", key, key, key)
			sfzContent += fmt.Sprintf("transpose=%d\n", transpose)
			if tune != 0 {
				sfzContent += fmt.Sprintf("tune=%d\n", tune)
			}
			if cutoff >= 0 {
				sfzContent += fmt.Sprintf("cutoff=%d\n", cutoff)
			}
			if resonance >= 0 {
				sfzContent += fmt.Sprintf("resonance=%d\n", resonance)
			}
			if mutegroup >= 0 {
				sfzContent += fmt.Sprintf("mutegroup=%d\n", mutegroup)
			}
			if oneShot {
				sfzContent += "loop_mode=one_shot\n"
			}
			sfzContent += "\n"
		}

		sfzPath := filepath.Join(dir, "test.sfz")
		if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
			t.Fatalf("WriteFile sfz: %v", err)
		}

		rates := []uint32{36000, 18000, 9000}
		rate := rates[nextByte()%3]

		outPath := filepath.Join(dir, "out.fzf")
		if err := Convert(context.Background(), sfzPath, outPath, rate, false); err != nil {
			t.Fatalf("Convert: %v", err)
		}

		info, err := fzfinfo.Parse(outPath)
		if err != nil {
			t.Fatalf("fzfinfo.Parse: %v", err)
		}
		if info.VoiceCount != nRegions {
			t.Fatalf("voice count %d != %d", info.VoiceCount, nRegions)
		}

		outData, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if len(outData)%disk.SectorSize != 0 {
			t.Fatalf("output size %d not sector-aligned", len(outData))
		}

		unpackDir := filepath.Join(dir, "unpacked")
		if err := voiceunpack.Unpack(outPath, unpackDir); err != nil {
			t.Fatalf("Unpack: %v", err)
		}

		entries, err := os.ReadDir(unpackDir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		for idx, e := range entries {
			fzvPath := filepath.Join(unpackDir, e.Name())
			params, err := fzvinfo.Parse(fzvPath)
			if err != nil {
				t.Fatalf("fzvinfo.Parse %s: %v", e.Name(), err)
			}
			fzvData, err := os.ReadFile(fzvPath)
			if err != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), err)
			}
			if _, _, err := voiceextract.Decode(fzvData); err != nil {
				t.Fatalf("voiceextract.Decode %s: %v", e.Name(), err)
			}

			if idx < len(metas) {
				if metas[idx].cutoff >= 0 {
					want := uint8(min(metas[idx].cutoff, 127)) //nolint:gosec // G115: fuzz value bounded by modulo above
					if params.FilterCutoff != want {
						t.Errorf("voice %d cutoff: got %d, want %d", idx, params.FilterCutoff, want)
					}
				}
				if metas[idx].resonance >= 0 {
					want := uint8(min(metas[idx].resonance, 127)) //nolint:gosec // G115: fuzz value bounded by modulo above
					if params.FilterQ != want {
						t.Errorf("voice %d resonance: got %d, want %d", idx, params.FilterQ, want)
					}
				}
			}
		}
	})
}
