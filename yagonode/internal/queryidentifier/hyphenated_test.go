package queryidentifier

import "testing"

func TestParseHyphenated(t *testing.T) {
	for _, text := range []string{"D9-2X", " d9‐2x "} {
		if got := ParseHyphenated(text); got != (Hyphenated{First: "d9", Last: "2x"}) {
			t.Errorf("ParseHyphenated(%q) = %v", text, got)
		}
	}
	for _, text := range []string{"D9 2X", "state-of", "D9-module", "module-2X", "D9 -2X", "D9- 2X", "D9--2X", "D9-2X guide", "-D9-2X"} {
		if got := ParseHyphenated(text); got != (Hyphenated{}) {
			t.Errorf("ParseHyphenated(%q) = %v", text, got)
		}
	}
}
