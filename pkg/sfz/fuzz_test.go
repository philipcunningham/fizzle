package sfz

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParse(f *testing.F) {
	f.Add("<region> sample=test.wav lokey=36 hikey=48")
	f.Add("<group> lovel=1 hivel=127\n<region> sample=a.wav")
	f.Add("")
	f.Add("<control>\n<global>\n<group>\n<region>")
	f.Add("<region> sample=test.wav pitch_keycenter=60 lokey=0 hikey=127 lovel=1 hivel=127")
	f.Add("// comment\n/* block\ncomment */\n<region> sample=x.wav")
	f.Add("<group> lokey=36 hikey=48\n<region> sample=a.wav\n<region> sample=b.wav")
	seedPath := "../../testdata/synthetic/JUNGLISM.sfz"
	if data, err := os.ReadFile(seedPath); err == nil {
		f.Add(string(data))
	}
	f.Fuzz(func(t *testing.T, content string) {
		dir := t.TempDir()
		sfzPath := filepath.Join(dir, "test.sfz")
		os.WriteFile(sfzPath, []byte(content), 0644) //nolint:errcheck
		regions, _, _ := Parse(sfzPath)
		// If Parse returned regions, every region must have a sample path
		// and well-ordered key and velocity ranges. The parser is allowed
		// to reject malformed input, but anything it accepts must be
		// coherent enough for sfzconvert to consume.
		for i, r := range regions {
			if r.Sample == "" {
				t.Fatalf("region %d has empty Sample after Parse", i)
			}
			if r.LoKey > r.HiKey {
				t.Fatalf("region %d has LoKey=%d > HiKey=%d", i, r.LoKey, r.HiKey)
			}
			if r.LoVel > r.HiVel {
				t.Fatalf("region %d has LoVel=%d > HiVel=%d", i, r.LoVel, r.HiVel)
			}
		}
	})
}
