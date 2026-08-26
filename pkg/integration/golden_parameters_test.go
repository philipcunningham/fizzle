package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"

	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

func TestJUNGLISMGoldenChecksums(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()

	fzf36k := filepath.Join(dir, "junglism-36k.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzf36k, 36000, false); err != nil {
		t.Fatalf("Convert 36kHz: %v", err)
	}

	fzfFit := filepath.Join(dir, "junglism-fit.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfFit, 36000, true); err != nil {
		t.Fatalf("Convert fit-to-disk: %v", err)
	}

	t.Run("FZF checksums", func(t *testing.T) {
		t.Parallel()
		fzfChecksums := []struct {
			name string
			path string
			want string
		}{
			{"36kHz full", fzf36k, "b695e1e8d06d5fd4e6d1228355cf89a5a2d60959e4716b37cb96f90e83251de6"},
			{"fit-to-disk", fzfFit, "022a067653e458281316a03c197d3d3756d7f873d7029e1d8c60a185d5233f50"},
		}
		for _, tc := range fzfChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})

	t.Run("disk image checksums", func(t *testing.T) {
		t.Parallel()
		imgFit := filepath.Join(dir, "junglism-fit.img")
		if err := diskformat.Format(imgFit, "JUNGLISM FIT"); err != nil {
			t.Fatal(err)
		}
		if err := diskadd.Add(imgFit, fzfFit, 0); err != nil {
			t.Fatal(err)
		}

		imgChecksums := []struct {
			name string
			path string
			want string
		}{
			{"fit-to-disk image", imgFit, "41ad1cde8a57b4e8b16c7820b7f83966afc3b6b10256bd9148330684e925c184"},
		}
		for _, tc := range imgChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})

	t.Run("split disk image checksums", func(t *testing.T) {
		t.Parallel()
		splitPrefix := filepath.Join(dir, "JUNGLISM")
		if err := sfzconvert.ConvertMultiDisk(context.Background(), junglismSFZ, splitPrefix, 36000); err != nil {
			t.Fatalf("ConvertMultiDisk: %v", err)
		}
		imgSplit1 := splitPrefix + "-1.img"
		imgSplit2 := splitPrefix + "-2.img"

		splitChecksums := []struct {
			name string
			path string
			want string
		}{
			{"split disk 1 image", imgSplit1, "67e6aa4d6d8c3e2034bb4d2a892da14b77bd1346f0068bbcaace49b18a333c21"},
			{"split disk 2 image", imgSplit2, "67d80910ff3c71665551890022e163ed643a58a6865c0bd30ffbba59b32f25c6"},
		}
		for _, tc := range splitChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})
}

func TestBRASSGoldenChecksums(t *testing.T) {
	skipShort(t)
	t.Parallel()

	t.Run("disk image", func(t *testing.T) {
		t.Parallel()
		got := fileSHA256(t, brassImg)
		want := "d40bb77ada4c2c875e142fa7f1b5dd845bcefa0a1f0e8562faea3413412a02f5"
		if got != want {
			t.Errorf("BRASS.img SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
		}
	})

	t.Run("extracted FZF", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "brass.fzf")
		if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
			t.Fatalf("disk get: %v", err)
		}
		got := fileSHA256(t, fzfPath)
		want := "36772600a3b9502c3ed44330dbe1a11dcd95cb9ed1c6ed583483c00d68f10199"
		if got != want {
			t.Errorf("BRASS FZF SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
		}
	})
}

// extractAndParseVoice extracts a named voice from a disk image's full dump
// and returns its parsed parameters. The pipeline exercises diskget, voiceunpack,
// and fzvinfo.Parse in sequence.
func extractAndParseVoice(t *testing.T, imgPath, voiceName string) *fzvinfo.VoiceParams {
	t.Helper()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "dump.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}
	fzvPath := filepath.Join(unpackDir, voiceName+".fzv")
	params, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", voiceName, err)
	}
	return params
}

// extractAndParseStandaloneVoice extracts a standalone voice file (not a full
// dump) from a disk image and returns its parsed parameters.
func extractAndParseStandaloneVoice(t *testing.T, imgPath, diskName string) *fzvinfo.VoiceParams {
	t.Helper()
	dir := t.TempDir()
	fzvPath := filepath.Join(dir, diskName+".fzv")
	if err := diskget.Get(imgPath, diskName, fzvPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	params, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", diskName, err)
	}
	return params
}

func TestBrassVoiceFilterEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, brassImg, "BRASS1 D3 1")

	if p.Name != "BRASS1 D3 1" {
		t.Errorf("Name = %q, want BRASS1 D3 1", p.Name)
	}
	if p.SampleRate != 36000 {
		t.Errorf("SampleRate = %d, want 36000", p.SampleRate)
	}
	if p.FilterCutoff != 88 {
		t.Errorf("FilterCutoff = %d, want 88", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}

	if p.DCADefault {
		t.Error("expected DCADefault=false for BRASS voice")
	}
	if p.DCASustain != 2 {
		t.Errorf("DCASustain = %d, want 2", p.DCASustain)
	}
	if p.DCAEnd != 3 {
		t.Errorf("DCAEnd = %d, want 3", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCAStops[0] != 218 {
		t.Errorf("DCAStops[0] = %d, want 218", p.DCAStops[0])
	}

	if p.DCFDefault {
		t.Error("expected DCFDefault=false for BRASS voice")
	}
	if p.DCFSustain != 1 {
		t.Errorf("DCFSustain = %d, want 1", p.DCFSustain)
	}
	if p.DCFEnd != 2 {
		t.Errorf("DCFEnd = %d, want 2", p.DCFEnd)
	}
	if p.DCFRates[0] != 127 {
		t.Errorf("DCFRates[0] = %d, want 127", p.DCFRates[0])
	}
	if p.DCFStops[0] != 66 {
		t.Errorf("DCFStops[0] = %d, want 66", p.DCFStops[0])
	}
	if p.DCFStops[1] != 56 {
		t.Errorf("DCFStops[1] = %d, want 56", p.DCFStops[1])
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestBrassFilterCutoffPerVoice(t *testing.T) {
	skipShort(t)
	t.Parallel()
	cases := []struct {
		name   string
		cutoff uint8
	}{
		{"BRASS1 D3 1", 88},
		{"BRASS1 D3 2", 90},
		{"BRASS1 G#3", 92},
		{"BRASS1 D4", 94},
		{"BRASS1 G#4", 96},
	}

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := fzvinfo.Parse(filepath.Join(unpackDir, tc.name+".fzv"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if p.FilterCutoff != tc.cutoff {
				t.Errorf("FilterCutoff = %d, want %d", p.FilterCutoff, tc.cutoff)
			}
			if p.FilterQ != 0 {
				t.Errorf("FilterQ = %d, want 0", p.FilterQ)
			}
			if p.DCFDefault {
				t.Error("expected DCFDefault=false")
			}
		})
	}
}

func TestTechnoVoiceResonanceAndEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, technoImg, "COWBELL")

	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 100 {
		t.Errorf("FilterQ = %d, want 100 (displays as resonance=6)", p.FilterQ)
	}

	if p.DCADefault {
		t.Error("expected DCADefault=false")
	}
	if p.DCASustain != 7 {
		t.Errorf("DCASustain = %d, want 7", p.DCASustain)
	}
	if p.DCAEnd != 2 {
		t.Errorf("DCAEnd = %d, want 2", p.DCAEnd)
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestTechnoMetalBellEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, technoImg, "METAL-BELL")

	if p.DCASustain != 1 {
		t.Errorf("DCASustain = %d, want 1", p.DCASustain)
	}
	if p.DCAEnd != 2 {
		t.Errorf("DCAEnd = %d, want 2", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCARates[1] != 253 {
		t.Errorf("DCARates[1] = %d, want 253 (displays as -125)", p.DCARates[1])
	}
	if p.DCARates[2] != 253 {
		t.Errorf("DCARates[2] = %d, want 253 (displays as -125)", p.DCARates[2])
	}
	if p.DCAStops[0] != 249 {
		t.Errorf("DCAStops[0] = %d, want 249", p.DCAStops[0])
	}

	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}
}

func TestHooverVoiceParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseStandaloneVoice(t, hooverImg, "HOOVER")

	if p.SampleRate != 36000 {
		t.Errorf("SampleRate = %d, want 36000", p.SampleRate)
	}
	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true (hardware idle pattern matches fizzle defaults)")
	}
	if p.DCASustain != 0 {
		t.Errorf("DCASustain = %d, want 0", p.DCASustain)
	}
	if p.DCAEnd != 7 {
		t.Errorf("DCAEnd = %d, want 7", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCARates[1] != 192 {
		t.Errorf("DCARates[1] = %d, want 192 (hardware idle: -64)", p.DCARates[1])
	}
	if p.DCAStops[0] != 255 {
		t.Errorf("DCAStops[0] = %d, want 255", p.DCAStops[0])
	}
	if p.DCAStops[1] != 0 {
		t.Errorf("DCAStops[1] = %d, want 0", p.DCAStops[1])
	}

	if p.PlaybackMode != disk.PlaybackModeNameNormal {
		t.Errorf("PlaybackMode = %q, want %q", p.PlaybackMode, disk.PlaybackModeNameNormal)
	}
	if p.HasActiveLoop {
		t.Error("expected HasActiveLoop=false for one-shot voice")
	}

	if p.DCFDefault {
		t.Error("expected DCFDefault=false (hardware DCF pattern differs from fizzle defaults)")
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestStabVoiceFilterParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, stabImg, "STAB")

	if p.FilterCutoff != 96 {
		t.Errorf("FilterCutoff = %d, want 96", p.FilterCutoff)
	}
	if p.FilterQ != 43 {
		t.Errorf("FilterQ = %d, want 43 (displays as resonance=2)", p.FilterQ)
	}

	if p.DCFSustain != 7 {
		t.Errorf("DCFSustain = %d, want 7 (holds filter open indefinitely)", p.DCFSustain)
	}
	if p.DCFEnd != 7 {
		t.Errorf("DCFEnd = %d, want 7", p.DCFEnd)
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true (hardware idle pattern matches fizzle defaults)")
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestPadLFOImageChecksum(t *testing.T) {
	skipShort(t)
	t.Parallel()
	got := fileSHA256(t, padLFOImg)
	want := "e755265a96f671530a3eb8e4d1972ce934efb325ee195eba8258a272a34642ef"
	if got != want {
		t.Errorf("PAD-LFO.img SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
	}
}

func TestPadLFOVoiceParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, padLFOImg, "PAD")

	if p.Name != "PAD" {
		t.Errorf("Name = %q, want PAD", p.Name)
	}
	if p.SampleRate != 18000 {
		t.Errorf("SampleRate = %d, want 18000", p.SampleRate)
	}
	if p.FilterCutoff != 64 {
		t.Errorf("FilterCutoff = %d, want 64", p.FilterCutoff)
	}
	if p.FilterQ>>4 != 7 {
		t.Errorf("Resonance = %d, want 7", p.FilterQ>>4)
	}

	if p.LFOWaveform != "Sine" {
		t.Errorf("LFOWaveform = %q, want Sine", p.LFOWaveform)
	}
	if p.LFORate != 20 {
		t.Errorf("LFORate = %d, want 20", p.LFORate)
	}
	if p.LFOAttack != 127 {
		t.Errorf("LFOAttack = %d, want 127", p.LFOAttack)
	}
	if p.LFODelay != 0 {
		t.Errorf("LFODelay = %d, want 0", p.LFODelay)
	}
	if p.LFODepthPitch != 0 {
		t.Errorf("LFODepthPitch = %d, want 0", p.LFODepthPitch)
	}
	if p.LFODepthAmp != 0 {
		t.Errorf("LFODepthAmp = %d, want 0", p.LFODepthAmp)
	}
	if p.LFODepthFilter != 50 {
		t.Errorf("LFODepthFilter = %d, want 50", p.LFODepthFilter)
	}
	if p.LFOPhaseSync {
		t.Error("expected LFOPhaseSync=false")
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true for fizzle-generated voice")
	}
}
