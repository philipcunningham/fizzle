package diskfs

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func TestBuildDISContiguous(t *testing.T) {
	t.Parallel()
	dis := buildDIS(2, []int{3, 4, 5, 6}, 0, 1, 3)
	if len(dis.Extents) != 1 || dis.Extents[0] != ([2]uint16{2, 6}) {
		t.Fatalf("extents = %v, want [[2 6]]", dis.Extents)
	}
	if dis.VoiceCount != 1 || dis.WaveCount != 3 {
		t.Fatalf("counts = voices %d, waves %d; want 1 and 3", dis.VoiceCount, dis.WaveCount)
	}
}

func TestBuildDISNonContiguous(t *testing.T) {
	t.Parallel()
	dis := buildDIS(2, []int{3, 4, 10, 11}, 0, 1, 3)
	want := [][2]uint16{{2, 4}, {10, 11}}
	if len(dis.Extents) != len(want) {
		t.Fatalf("extents = %v, want %v", dis.Extents, want)
	}
	for i := range want {
		if dis.Extents[i] != want[i] {
			t.Fatalf("extent %d = %v, want %v", i, dis.Extents[i], want[i])
		}
	}
}

func TestBuildDISEmptySectors(t *testing.T) {
	t.Parallel()
	dis := buildDIS(0, nil, 0, 0, 0)
	decoded, err := disk.DecodeDisSector(disk.EncodeDisSector(dis))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Extents) != 0 {
		t.Fatalf("extents = %v, want none", decoded.Extents)
	}
}
