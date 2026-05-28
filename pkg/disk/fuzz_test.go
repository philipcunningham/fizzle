package disk

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"strings"
	"testing"
)

func FuzzReadImage(f *testing.F) {
	f.Add(make([]byte, ImageSize))
	f.Add([]byte{})
	f.Add(make([]byte, 1024))
	for _, name := range []string{"HOOVER.img", "BRASS.img", "TECHNO.img", "STAB.img", "PAD-LFO.img"} {
		if data, err := os.ReadFile("../../testdata/synthetic/" + name); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := ReadImage(bytes.NewReader(data))
		if err != nil {
			return
		}
		if len(img.Bytes()) != ImageSize {
			t.Fatalf("ReadImage success but image size %d != %d", len(img.Bytes()), ImageSize)
		}
		if _, err := img.Directory(); err != nil {
			t.Fatalf("ReadImage success but Directory() failed: %v", err)
		}
	})
}

func FuzzRateByteToDisplayRange(f *testing.F) {
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(64))
	f.Add(uint8(127))
	f.Add(uint8(0x80))
	f.Add(uint8(0xC0))
	f.Add(uint8(0xFF))
	f.Fuzz(func(t *testing.T, b uint8) {
		d := RateByteToDisplay(b)
		if d < 0 || d > DisplayMax {
			t.Errorf("RateByteToDisplay(0x%02x) = %d, out of 0..%d", b, d, DisplayMax)
		}
		rising := RateByteToDisplay(b &^ RateSignBit)
		falling := RateByteToDisplay(b | RateSignBit)
		if rising != falling {
			t.Errorf("sign bit affects display: 0x%02x -> %d vs %d", b, rising, falling)
		}
	})
}

func FuzzStopByteToDisplayRange(f *testing.F) {
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(127))
	f.Add(uint8(128))
	f.Add(uint8(255))
	f.Fuzz(func(t *testing.T, b uint8) {
		d := StopByteToDisplay(b)
		if d < 0 || d > DisplayMax {
			t.Errorf("StopByteToDisplay(%d) = %d, out of 0..%d", b, d, DisplayMax)
		}
	})
}

func FuzzRateDisplayToByteRoundTrip(f *testing.F) {
	for d := range DisplayMax + 1 {
		f.Add(uint8(d)) //nolint:gosec // d is 0..99
	}
	f.Fuzz(func(t *testing.T, raw uint8) {
		d := int(raw) % (DisplayMax + 1)
		b := RateDisplayToByte(d)
		back := RateByteToDisplay(b)
		if back != d {
			t.Errorf("rate round-trip: %d -> 0x%02x -> %d", d, b, back)
		}
	})
}

func FuzzStopDisplayToByteRoundTrip(f *testing.F) {
	for d := range DisplayMax + 1 {
		f.Add(uint8(d)) //nolint:gosec // d is 0..99
	}
	f.Fuzz(func(t *testing.T, raw uint8) {
		d := int(raw) % (DisplayMax + 1)
		b := StopDisplayToByte(d)
		back := StopByteToDisplay(b)
		if back != d {
			t.Errorf("stop round-trip: %d -> %d -> %d", d, b, back)
		}
	})
}

func FuzzRateByteDisplayMonotonic(f *testing.F) {
	f.Add(uint8(0), uint8(1))
	f.Add(uint8(63), uint8(64))
	f.Add(uint8(126), uint8(127))
	f.Fuzz(func(t *testing.T, a, b uint8) {
		magA := a & RateMagMask
		magB := b & RateMagMask
		if magA <= magB {
			dA := RateByteToDisplay(magA)
			dB := RateByteToDisplay(magB)
			if dA > dB {
				t.Errorf("not monotonic: mag %d -> %d, mag %d -> %d", magA, dA, magB, dB)
			}
		}
	})
}

func FuzzStopByteDisplayMonotonic(f *testing.F) {
	f.Add(uint8(0), uint8(1))
	f.Add(uint8(127), uint8(128))
	f.Add(uint8(254), uint8(255))
	f.Fuzz(func(t *testing.T, a, b uint8) {
		if a <= b {
			dA := StopByteToDisplay(a)
			dB := StopByteToDisplay(b)
			if dA > dB {
				t.Errorf("not monotonic: byte %d -> %d, byte %d -> %d", a, dA, b, dB)
			}
		}
	})
}

func FuzzDirEntryRoundTrip(f *testing.F) {
	f.Add([]byte("HOOVER      "), uint8(1), uint8(0), uint16(2))
	f.Add(make([]byte, LabelSize), uint8(0), uint8(0), uint16(0))
	f.Add([]byte("ABCDEFGHIJKL"), uint8(5), uint8(1), uint16(1279))
	f.Fuzz(func(t *testing.T, nameBytes []byte, ft uint8, diskNum uint8, disSector uint16) {
		if len(nameBytes) < LabelSize {
			return
		}
		ft %= 6
		var name [LabelSize]byte
		copy(name[:], nameBytes[:LabelSize])
		e := DirEntry{
			Name:      name,
			FileType:  FileType(ft),
			DiskNum:   diskNum,
			DisSector: disSector,
		}
		encoded := EncodeDirEntry(e)
		if len(encoded) != DirEntrySize {
			t.Fatalf("EncodeDirEntry length = %d, want %d", len(encoded), DirEntrySize)
		}
		decoded, err := DecodeDirEntry(encoded)
		if err != nil {
			t.Fatalf("DecodeDirEntry: %v", err)
		}
		if decoded.Name != e.Name {
			t.Errorf("Name mismatch: got %v, want %v", decoded.Name, e.Name)
		}
		if decoded.FileType != e.FileType {
			t.Errorf("FileType: got %d, want %d", decoded.FileType, e.FileType)
		}
		if decoded.DiskNum != e.DiskNum {
			t.Errorf("DiskNum: got %d, want %d", decoded.DiskNum, e.DiskNum)
		}
		if decoded.DisSector != e.DisSector {
			t.Errorf("DisSector: got %d, want %d", decoded.DisSector, e.DisSector)
		}
	})
}

func FuzzDisSectorRoundTrip(f *testing.F) {
	f.Add(uint16(2), uint16(10), uint16(0), uint16(1), uint16(9))
	f.Add(uint16(2), uint16(2), uint16(1), uint16(1), uint16(1))
	f.Add(uint16(0), uint16(0), uint16(0), uint16(0), uint16(0))
	f.Fuzz(func(t *testing.T, start, end, bankCount, voiceCount, waveCount uint16) {
		d := DisSector{
			BankCount:  bankCount,
			VoiceCount: voiceCount,
			WaveCount:  waveCount,
		}
		// Skip fuzz inputs whose extent falls outside the valid data range
		// (DecodeDisSector now rejects extents that touch the reserved
		// sectors or run off the end of the disk).
		if start > 0 || end > 0 {
			if int(start) < ReservedSectors || int(end) < ReservedSectors ||
				int(start) >= SectorCount || int(end) >= SectorCount ||
				end < start {
				return
			}
			d.Extents = [][2]uint16{{start, end}}
		}
		encoded := EncodeDisSector(d)
		if len(encoded) != SectorSize {
			t.Fatalf("EncodeDisSector length = %d, want %d", len(encoded), SectorSize)
		}
		decoded, err := DecodeDisSector(encoded)
		if err != nil {
			t.Fatalf("DecodeDisSector: %v", err)
		}
		if decoded.BankCount != d.BankCount {
			t.Errorf("BankCount: got %d, want %d", decoded.BankCount, d.BankCount)
		}
		if decoded.VoiceCount != d.VoiceCount {
			t.Errorf("VoiceCount: got %d, want %d", decoded.VoiceCount, d.VoiceCount)
		}
		if decoded.WaveCount != d.WaveCount {
			t.Errorf("WaveCount: got %d, want %d", decoded.WaveCount, d.WaveCount)
		}
		if len(d.Extents) > 0 {
			if len(decoded.Extents) < 1 {
				t.Fatalf("expected at least 1 extent, got %d", len(decoded.Extents))
			}
			if decoded.Extents[0] != d.Extents[0] {
				t.Errorf("Extent[0]: got %v, want %v", decoded.Extents[0], d.Extents[0])
			}
		}
	})
}

func FuzzPadLabelTrimRoundTrip(f *testing.F) {
	f.Add("HOOVER")
	f.Add("")
	f.Add("EXACTLY12CHR")
	f.Add("TOOLONGSTRING")
	f.Add("A B C")
	f.Add("   ")
	f.Fuzz(func(t *testing.T, s string) {
		padded := PadLabel(s)
		trimmed := TrimPadded(padded[:])
		truncated := s
		if len(truncated) > LabelSize {
			truncated = truncated[:LabelSize]
		}
		truncated = strings.TrimRight(truncated, " ")
		if trimmed != truncated {
			t.Errorf("PadLabel/TrimPadded round-trip: input=%q, got=%q, want=%q", s, trimmed, truncated)
		}
		padded2 := PadLabel(trimmed)
		trimmed2 := TrimPadded(padded2[:])
		if trimmed2 != trimmed {
			t.Errorf("idempotence failed: %q -> %q -> %q", s, trimmed, trimmed2)
		}
	})
}

func FuzzSectorsNeededPadToSectorRelation(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(SectorSize - 1)
	f.Add(SectorSize)
	f.Add(SectorSize + 1)
	f.Add(ImageSize)
	f.Fuzz(func(t *testing.T, n int) {
		if n < 0 {
			return
		}
		sn := SectorsNeeded(n)
		ps := PadToSector(n)
		if sn*SectorSize != ps {
			t.Errorf("SectorsNeeded(%d)*SectorSize=%d != PadToSector(%d)=%d", n, sn*SectorSize, n, ps)
		}
		if ps < n {
			t.Errorf("PadToSector(%d) = %d, less than input", n, ps)
		}
		if ps%SectorSize != 0 {
			t.Errorf("PadToSector(%d) = %d, not sector-aligned", n, ps)
		}
		if PadToSector(ps) != ps {
			t.Errorf("PadToSector not idempotent: PadToSector(%d) = %d, PadToSector(%d) = %d", n, ps, ps, PadToSector(ps))
		}
	})
}

func FuzzVoiceSlotOffsetStrictlyIncreasing(f *testing.F) {
	f.Add(SectorSize, 0, 1)
	f.Add(SectorSize, 3, 4)
	f.Add(2*SectorSize, 0, 63)
	f.Fuzz(func(t *testing.T, base int, i, j int) {
		if base < 0 || i < 0 || j < 0 || i >= MaxVoices || j >= MaxVoices || i >= j {
			return
		}
		oi := VoiceSlotOffset(base, i)
		oj := VoiceSlotOffset(base, j)
		if oj <= oi {
			t.Errorf("VoiceSlotOffset(%d, %d)=%d >= VoiceSlotOffset(%d, %d)=%d", base, j, oj, base, i, oi)
		}
		if oi < base {
			t.Errorf("VoiceSlotOffset(%d, %d)=%d < base", base, i, oi)
		}
		if j == i+1 && oj-oi < VoicePackSize {
			t.Errorf("adjacent slots overlap: i=%d at %d, j=%d at %d, gap=%d < VoicePackSize=%d", i, oi, j, oj, oj-oi, VoicePackSize)
		}
	})
}

func FuzzForEachSamplePointerPreservation(f *testing.F) {
	f.Add(make([]byte, VoicePackSize))
	voice := make([]byte, VoicePackSize)
	for i := range voice {
		voice[i] = byte(i)
	}
	f.Add(voice)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < LoopPointerRangeEnd {
			return
		}
		original := make([]byte, len(data))
		copy(original, data)
		ForEachSamplePointer(data, func([]byte, SamplePointerKind) {})
		if !bytes.Equal(data, original) {
			t.Error("no-op ForEachSamplePointer modified voice data")
		}
	})
}

func FuzzDecodeDisSectorNoPanic(f *testing.F) {
	f.Add(make([]byte, SectorSize))
	filled := make([]byte, SectorSize)
	for i := range filled {
		filled[i] = 0xff
	}
	f.Add(filled)
	maxExtents := make([]byte, SectorSize)
	for i := 0; i < DisTailOffset; i += ExtentEntrySize {
		binary.LittleEndian.PutUint16(maxExtents[i:], uint16(i+2))
		binary.LittleEndian.PutUint16(maxExtents[i+2:], uint16(i+3))
	}
	f.Add(maxExtents)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < SectorSize {
			return
		}
		dis, err := DecodeDisSector(data[:SectorSize])
		if err != nil {
			return
		}
		// Decode-encode-decode must be idempotent: any DisSector we get back
		// from arbitrary bytes must re-encode to bytes that decode to the
		// same DisSector.
		again, err := DecodeDisSector(EncodeDisSector(dis))
		if err != nil {
			t.Fatalf("re-decoding encoded DisSector failed: %v", err)
		}
		if !reflect.DeepEqual(dis, again) {
			t.Fatalf("decode/encode/decode not idempotent:\nfirst:  %+v\nsecond: %+v", dis, again)
		}
	})
}

func FuzzRateIndexRoundTrip(f *testing.F) {
	f.Add(uint32(36000))
	f.Add(uint32(18000))
	f.Add(uint32(9000))
	f.Add(uint32(44100))
	f.Add(uint32(0))
	f.Fuzz(func(t *testing.T, rate uint32) {
		idx, ok := RateIndexFor(rate)
		if !ok {
			sr := SampleRate(uint8(rate % 256))
			if sr != 0 {
				idx2, ok2 := RateIndexFor(sr)
				if !ok2 {
					t.Errorf("SampleRate(%d) = %d, but RateIndexFor(%d) = false", rate%256, sr, sr)
				}
				if SampleRate(idx2) != sr {
					t.Errorf("RateIndexFor(%d) = %d, SampleRate(%d) = %d, want %d", sr, idx2, idx2, SampleRate(idx2), sr)
				}
			}
			return
		}
		back := SampleRate(idx)
		if back != rate {
			t.Errorf("RateIndexFor(%d) = %d, SampleRate(%d) = %d, want %d", rate, idx, idx, back, rate)
		}
	})
}
