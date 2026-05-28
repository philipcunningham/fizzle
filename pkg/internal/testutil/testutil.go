// Package testutil provides shared test helpers.
package testutil

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/wav"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// CaptureLog redirects zerolog output to a buffer for the duration of the
// test using the same console formatting as production. Callers must not
// use t.Parallel because this mutates the global logger.
func CaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldLogger := log.Logger
	oldLevel := zerolog.GlobalLevel()
	logger.InitWithWriter(true, &buf)
	t.Cleanup(func() {
		log.Logger = oldLogger
		zerolog.SetGlobalLevel(oldLevel)
	})
	return &buf
}

// BufHasWarnContaining reports whether buf contains a warn-level log line with substr.
func BufHasWarnContaining(buf *bytes.Buffer, s string) bool {
	out := buf.String()
	return strings.Contains(out, "WARN") && strings.Contains(out, s)
}

// WriteTestWAV creates a WAV file at path with the given sample rate and number of samples.
func WriteTestWAV(t *testing.T, path string, sampleRate uint32, nSamples int) {
	t.Helper()
	samples := make([]int16, nSamples)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}
	f := &wav.File{SampleRate: sampleRate, Samples: samples, LoopStart: -1, LoopEnd: -1}
	var buf bytes.Buffer
	if err := wav.Write(&buf, f); err != nil {
		t.Fatalf("writing test WAV: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("saving test WAV: %v", err)
	}
}

// MakeTestVoice builds a minimal FZV byte slice with the given name and sample count.
func MakeTestVoice(name string, sampleCount int) []byte {
	data := make([]byte, disk.SectorSize+sampleCount*2)
	paddedName := disk.PadLabel(name)
	copy(data[disk.VoiceNameOffset:], paddedName[:])
	binary.LittleEndian.PutUint32(data[0x00:], 0)
	binary.LittleEndian.PutUint32(data[0x04:], uint32(sampleCount)) //nolint:gosec // G115: test value fits target type
	binary.LittleEndian.PutUint32(data[0x08:], 0)
	binary.LittleEndian.PutUint32(data[0x0c:], uint32(sampleCount)) //nolint:gosec // G115: test value fits target type
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	for i := range sampleCount {
		binary.LittleEndian.PutUint16(data[disk.SectorSize+i*2:], uint16(i%32768))
	}
	return data
}

// MakeTestDisk creates a formatted disk image with a single voice file and
// returns the image path. The voice file is 2 sectors in size.
func MakeTestDisk(t *testing.T, label, voiceName string) string {
	t.Helper()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, label+".img")
	fzvPath := filepath.Join(dir, voiceName+".fzv")

	fzv := make([]byte, disk.SectorSize*2)
	padded := disk.PadLabel(voiceName)
	copy(fzv[disk.VoiceNameOffset:], padded[:])
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := diskformat.Format(imgPath, label); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}
	return imgPath
}

// Abs returns the absolute value of x.
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SyntheticImageOpts configures SyntheticImage. All fields are optional.
type SyntheticImageOpts struct {
	Label       string // disk label, max disk.LabelSize chars; defaults to "TEST"
	AllocatedAt []int  // sector indices to pre-mark allocated (beyond the format reservation)
}

// SyntheticImage builds a formatted, in-memory disk image as a byte slice with
// optional pre-allocated sectors. This is intended for edge-case unit tests
// that need specific disk states (fragmented CAT, near-full disk, etc.)
// without relying on real .img fixtures.
func SyntheticImage(t *testing.T, opts SyntheticImageOpts) []byte {
	t.Helper()
	if opts.Label == "" {
		opts.Label = "TEST"
	}
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "synthetic.img")
	if err := diskformat.Format(imgPath, opts.Label); err != nil {
		t.Fatalf("SyntheticImage: format: %v", err)
	}
	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("SyntheticImage: read: %v", err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("SyntheticImage: parse: %v", err)
	}
	for _, s := range opts.AllocatedAt {
		if err := img.CATSetAllocated(s); err != nil {
			t.Fatalf("SyntheticImage: CATSetAllocated(%d): %v", s, err)
		}
	}
	return img.Bytes()
}
