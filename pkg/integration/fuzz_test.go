package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskcopy"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"pgregory.net/rapid"
)

func FuzzVoiceEncodeDecodeRoundTrip(f *testing.F) {
	f.Add(uint8(0), []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0x7f})
	f.Add(uint8(1), []byte{0x00, 0x80, 0xff, 0x7f})
	f.Add(uint8(2), []byte{0x00, 0x00})
	f.Fuzz(func(t *testing.T, rateIdx uint8, sampleBytes []byte) {
		rateIdx %= 3
		nSamples := (len(sampleBytes) / 2) % 5000
		if nSamples == 0 {
			return
		}
		samples := make([]int16, nSamples)
		for i := range samples {
			samples[i] = bitconv.ReadInt16LE(sampleBytes[i*2:])
		}
		data := voiceimport.Encode(samples, rateIdx, "FUZZTEST", 0, voiceimport.NoLoop())
		rate, decoded, err := voiceextract.Decode(data)
		if err != nil {
			t.Fatalf("Decode after Encode: %v", err)
		}
		wantRate := disk.SampleRates[rateIdx]
		if rate != wantRate {
			t.Errorf("rate: got %d, want %d", rate, wantRate)
		}
		if len(decoded) != len(samples) {
			t.Fatalf("samples length: got %d, want %d", len(decoded), len(samples))
		}
		for i, s := range decoded {
			if s != samples[i] {
				t.Errorf("sample[%d]: got %d, want %d", i, s, samples[i])
				break
			}
		}
	})
}

func FuzzDiskImageRoundTrip(f *testing.F) {
	f.Add([]byte{2, 100, 200, 0, 1, 2, 3, 0, 1})
	f.Add([]byte{0, 50, 150, 3, 3, 3, 3, 1, 2, 0})
	f.Add([]byte{1, 80, 120, 2, 0, 1, 2, 3, 0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, seed []byte) {
		if len(seed) < 2 {
			return
		}

		nVoices := int(seed[0])%3 + 1
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
		imgPath := filepath.Join(dir, "test.img")
		if err := diskformat.Format(imgPath, "FUZZTEST"); err != nil {
			t.Fatalf("Format: %v", err)
		}

		type voiceFile struct {
			name string
			path string
		}
		voices := make([]voiceFile, nVoices)
		for i := range nVoices {
			nameByte := nextByte()
			countByte := nextByte()
			name := fmt.Sprintf("V%02X%02X%04d", nameByte, i, i)
			if len(name) > disk.LabelSize {
				name = name[:disk.LabelSize]
			}
			sampleCount := int(countByte)%500 + 10
			data := testutil.MakeTestVoice(name, sampleCount)
			fzvPath := filepath.Join(dir, fmt.Sprintf("voice%d.fzv", i))
			if err := os.WriteFile(fzvPath, data, 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			voices[i] = voiceFile{name: name, path: fzvPath}
		}

		img2Path := filepath.Join(dir, "test2.img")
		if err := diskformat.Format(img2Path, "FUZZ2"); err != nil {
			t.Fatalf("Format img2: %v", err)
		}

		addedVoices := 0
		iterations := min(len(seed), 100)
		for i := range iterations {
			op := seed[i] % 5

			switch op {
			case 0:
				if addedVoices >= nVoices {
					continue
				}
				err := diskadd.Add(imgPath, voices[addedVoices].path, 0)
				if err != nil {
					continue
				}
				addedVoices++

			case 1:
				if addedVoices == 0 {
					continue
				}
				idx := int(seed[i]) % addedVoices
				outPath := filepath.Join(dir, fmt.Sprintf("get%d.fzv", i))
				err := diskget.Get(imgPath, voices[idx].name, outPath)
				if err != nil {
					t.Fatalf("Get %q: %v", voices[idx].name, err)
				}
				data, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				if len(data) == 0 {
					t.Fatal("Get returned empty file")
				}
				orig, err := os.ReadFile(voices[idx].path)
				if err != nil {
					t.Fatalf("ReadFile orig: %v", err)
				}
				if !bytes.HasPrefix(data, orig) {
					t.Fatalf("Get %q: got %d bytes, want prefix of %d bytes", voices[idx].name, len(data), len(orig))
				}

			case 2:
				if addedVoices == 0 {
					continue
				}
				idx := int(seed[i]) % addedVoices
				err := diskcopy.Copy(imgPath, voices[idx].name, img2Path)
				if err != nil {
					continue
				}
				listing, err := disklist.Parse(img2Path)
				if err != nil {
					t.Fatalf("Parse img2: %v", err)
				}
				found := false
				for _, e := range listing.Entries {
					if e.Name == voices[idx].name {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("copied voice %q not found on destination disk", voices[idx].name)
				}

			case 3:
				_, err := disklist.Parse(imgPath)
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}

			case 4:
				if err := diskformat.Format(imgPath, "REFMT"); err != nil {
					continue
				}
				addedVoices = 0
			}

			img, err := disk.OpenImage(imgPath)
			if err != nil {
				t.Fatalf("ReadImage after op %d: %v", i, err)
			}
			if _, err := img.Directory(); err != nil {
				t.Fatalf("Directory after op %d: %v", i, err)
			}
			if len(img.Bytes()) != disk.ImageSize {
				t.Fatalf("image size %d != %d", len(img.Bytes()), disk.ImageSize)
			}
		}

		if addedVoices > 0 {
			listing, err := disklist.Parse(imgPath)
			if err != nil {
				t.Fatalf("final Parse: %v", err)
			}
			for _, e := range listing.Entries {
				outPath := filepath.Join(dir, "final-"+e.Name+".fzv")
				if err := diskget.Get(imgPath, e.Name, outPath); err != nil {
					t.Fatalf("final Get %q: %v", e.Name, err)
				}
				data, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatalf("final ReadFile: %v", err)
				}
				if len(data) == 0 {
					t.Fatalf("final Get %q returned empty", e.Name)
				}
			}
		}
	})
}

func TestDiskImageStateMachine(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "sm.img")
	img2Path := filepath.Join(dir, "sm2.img")
	if err := diskformat.Format(img2Path, "SM2"); err != nil {
		t.Fatalf("Format img2: %v", err)
	}

	type voice struct{ name, path string }
	voices := make([]voice, 0, 3)
	for i, name := range []string{"A", "B", "C"} {
		path := filepath.Join(dir, name+".fzv")
		if err := os.WriteFile(path, testutil.MakeTestVoice(name, 20+i*10), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		voices = append(voices, voice{name: name, path: path})
	}
	byName := func(n string) voice {
		for _, v := range voices {
			if v.name == n {
				return v
			}
		}
		return voice{}
	}
	names := []string{"A", "B", "C"}

	rapid.Check(t, func(t *rapid.T) {
		if err := diskformat.Format(imgPath, "SMTEST"); err != nil {
			t.Fatalf("Format: %v", err)
		}
		added := map[string]bool{}
		t.Repeat(map[string]func(*rapid.T){
			"Add": func(t *rapid.T) {
				n := rapid.SampledFrom(names).Draw(t, "name")
				if err := diskadd.Add(imgPath, byName(n).path, 0); err != nil {
					return
				}
				added[n] = true
			},
			"Get": func(t *rapid.T) {
				if len(added) == 0 {
					return
				}
				addedList := make([]string, 0, len(added))
				for n := range added {
					addedList = append(addedList, n)
				}
				n := rapid.SampledFrom(addedList).Draw(t, "name")
				out := filepath.Join(dir, "sm-get.fzv")
				if err := diskget.Get(imgPath, n, out); err != nil {
					t.Fatalf("Get %q: %v", n, err)
				}
				got, err := os.ReadFile(out)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				orig, err := os.ReadFile(byName(n).path)
				if err != nil {
					t.Fatalf("ReadFile orig: %v", err)
				}
				if !bytes.HasPrefix(got, orig) {
					t.Fatalf("Get %q: got %d bytes, want prefix of %d bytes", n, len(got), len(orig))
				}
			},
			"Copy": func(t *rapid.T) {
				if len(added) == 0 {
					return
				}
				addedList := make([]string, 0, len(added))
				for n := range added {
					addedList = append(addedList, n)
				}
				n := rapid.SampledFrom(addedList).Draw(t, "name")
				_ = diskcopy.Copy(imgPath, n, img2Path)
			},
			"List": func(t *rapid.T) {
				if _, err := disklist.Parse(imgPath); err != nil {
					t.Fatalf("Parse: %v", err)
				}
			},
			"Format": func(t *rapid.T) {
				if err := diskformat.Format(imgPath, "REFMT"); err != nil {
					t.Fatalf("Format: %v", err)
				}
				added = map[string]bool{}
			},
			"": func(t *rapid.T) {
				img, err := disk.OpenImage(imgPath)
				if err != nil {
					t.Fatalf("OpenImage: %v", err)
				}
				if _, err := img.Directory(); err != nil {
					t.Fatalf("Directory: %v", err)
				}
				if len(img.Bytes()) != disk.ImageSize {
					t.Fatalf("image size %d != %d", len(img.Bytes()), disk.ImageSize)
				}
			},
		})
	})
}
