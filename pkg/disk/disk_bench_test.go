package disk

import "testing"

// BenchmarkSector measures the per-call allocation of Sector(n). Read-only
// callers (disklist, diskget) pay this 1024-byte copy on every access.
func BenchmarkSector(b *testing.B) {
	img := &Image{}
	for i := range img.data {
		img.data[i] = byte(i & 0xff) //nolint:gosec // benchmark fill pattern
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s, err := img.Sector(2 + i%(SectorCount-2))
		if err != nil {
			b.Fatal(err)
		}
		_ = s
	}
}

// BenchmarkSectorRef measures the no-copy view returned by SectorRef. Should
// allocate zero bytes per call; compare against BenchmarkSector.
func BenchmarkSectorRef(b *testing.B) {
	img := &Image{}
	for i := range img.data {
		img.data[i] = byte(i & 0xff) //nolint:gosec // benchmark fill pattern
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s, err := img.SectorRef(2 + i%(SectorCount-2))
		if err != nil {
			b.Fatal(err)
		}
		_ = s
	}
}

// BenchmarkAllocateSectors measures the CAT scan and bitmap-update path on a
// nearly-empty disk. Worst-case it walks the full 1278 data sectors.
func BenchmarkAllocateSectors(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		img := &Image{}
		// Mark sectors 0 and 1 allocated as Format would.
		img.data[CATOffset] = 0x03
		_, err := img.AllocateSectors(64)
		if err != nil {
			b.Fatal(err)
		}
	}
}
