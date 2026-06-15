package sound

import "testing"

// TestEnvelopeStageRateStopMath pins the rate/stop display<->byte
// behaviour of the envelope stage fields so the SIMP-005 refactor
// (delegating to disk.RateByteToDisplay / RateDisplayToByte /
// StopByteToDisplay / StopDisplayToByte) is proven behaviour-preserving.
// Offsets are arbitrary; only the conversion math is under test.
func TestEnvelopeStageRateStopMath(t *testing.T) {
	t.Parallel()
	const rateAbs, stopAbs = 10, 20
	fields := envelopeStageFields(0, 0, 5, 6, rateAbs, stopAbs)
	rate, stop := fields[1], fields[2]
	if rate.label != "Rate" || stop.label != "Stop level" {
		t.Fatalf("unexpected field order: %q, %q", rate.label, stop.label)
	}
	mk := func() []byte { return make([]byte, 64) }

	// Rate read: magnitude (low 7 bits) maps to 0..99; sign bit ignored.
	for _, c := range []struct {
		b    byte
		want int
	}{{0, 0}, {64, 50}, {127, 99}, {0xC0, 50}} {
		d := mk()
		d[rateAbs] = c.b
		if got := rate.read(d); got != c.want {
			t.Errorf("rate.read(%#x) = %d, want %d", c.b, got, c.want)
		}
	}
	// Rate patch preserves the sign bit and writes the magnitude.
	d := mk()
	d[rateAbs] = 0x80 // falling, magnitude 0 (display 0)
	if ps := rate.patch(d, 50); len(ps) != 1 || ps[0].New[0] != 0xC0 {
		t.Errorf("rate.patch sign-preserve: got %+v, want New=0xC0", ps)
	}
	// Rate patch is a no-op when the display value is unchanged.
	d = mk()
	d[rateAbs] = 127
	if ps := rate.patch(d, 99); ps != nil {
		t.Errorf("rate.patch unchanged: got %+v, want nil", ps)
	}

	// Stop read: byte 0..255 maps to display 0..99.
	for _, c := range []struct {
		b    byte
		want int
	}{{0, 0}, {128, 50}, {255, 99}} {
		d := mk()
		d[stopAbs] = c.b
		if got := stop.read(d); got != c.want {
			t.Errorf("stop.read(%#x) = %d, want %d", c.b, got, c.want)
		}
	}
	// Stop patch: display 0..99 maps back to byte; 0 from empty is a no-op.
	for _, c := range []struct {
		disp int
		want byte
		nop  bool
	}{{0, 0, true}, {50, 127, false}, {99, 255, false}} {
		d := mk()
		ps := stop.patch(d, c.disp)
		if c.nop {
			if ps != nil {
				t.Errorf("stop.patch(%d): got %+v, want nil", c.disp, ps)
			}
			continue
		}
		if len(ps) != 1 || ps[0].New[0] != c.want {
			t.Errorf("stop.patch(%d): got %+v, want byte %#x", c.disp, ps, c.want)
		}
	}
}
