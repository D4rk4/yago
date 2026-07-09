package bytesize_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/bytesize"
)

func TestParseUnits(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"1B", 1},
		{"2KB", 2 << 10},
		{"3MB", 3 << 20},
		{"4GB", 4 << 30},
		{"1TB", 1 << 40},
		{" 5 MB ", 5 << 20},
		{"0B", 0},
	}
	for _, c := range cases {
		got, err := bytesize.Parse(c.raw)
		if err != nil {
			t.Fatalf("%q: %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("%q = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, raw := range []string{"100", "xxKB", "-1GB", ""} {
		if _, err := bytesize.Parse(raw); err == nil {
			t.Errorf("%q: expected error", raw)
		}
	}
}
