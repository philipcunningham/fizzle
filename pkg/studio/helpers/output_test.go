package helpers

import "testing"

func TestFormatOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   uint8
		want string
	}{
		{0xff, OutputAll},
		{0x00, "none"},
		{0x01, "1"},
		{0x80, "8"},
		{0x05, "1,3"},
	}
	for _, c := range cases {
		if got := FormatOutput(c.in); got != c.want {
			t.Errorf("FormatOutput(0x%02x) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want uint8
	}{
		{OutputAll, 0xff},
		{"ALL", 0xff},
		{"poly", 0xff},
		{"1", 0x01},
		{"8", 0x80},
		{"1,3", 0x05},
		{"3, 1", 0x05}, // whitespace and order are forgiving
	}
	for _, c := range cases {
		got, err := ParseOutput(c.in)
		if err != nil {
			t.Errorf("ParseOutput(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseOutput(%q) = 0x%02x, want 0x%02x", c.in, got, c.want)
		}
	}
}

func TestParseOutputErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "9", "0", "wat", "1,9"} {
		if _, err := ParseOutput(in); err == nil {
			t.Errorf("ParseOutput(%q) want error, got nil", in)
		}
	}
}
