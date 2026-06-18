package infrastructure

import "testing"

func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{
		"1B":    1,
		"1KB":   1 << 10,
		"512MB": 512 << 20,
		"1GB":   1 << 30,
		"2TB":   2 << 40,
		"0B":    0,
	}
	for raw, want := range cases {
		got, err := parseByteSize(raw)
		if err != nil {
			t.Fatalf("parseByteSize(%q): %v", raw, err)
		}
		if got != want {
			t.Errorf("parseByteSize(%q) = %d, want %d", raw, got, want)
		}
	}

	got, err := parseByteSize(" 1gb ")
	if err != nil {
		t.Fatalf("parseByteSize lowercase/padded: %v", err)
	}
	if got != 1<<30 {
		t.Errorf("parseByteSize(\" 1gb \") = %d, want %d", got, int64(1<<30))
	}
}

func TestParseByteSizeRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "abc", "10", "-5MB", "1.5GB"} {
		if _, err := parseByteSize(raw); err == nil {
			t.Errorf("parseByteSize(%q): expected error", raw)
		}
	}
}
