package yacywire

import (
	"errors"
	"testing"
)

func TestParsePeerType(t *testing.T) {
	for _, valid := range []string{"virgin", "junior", "senior", "principal"} {
		pt, err := ParsePeerType(valid)
		if err != nil {
			t.Errorf("ParsePeerType(%q) = %v", valid, err)
		}
		if pt.String() != valid {
			t.Errorf("String() = %q, want %q", pt, valid)
		}
	}
	if _, err := ParsePeerType("master"); !errors.Is(err, ErrInvalidPeerType) {
		t.Fatalf("ParsePeerType invalid = %v, want ErrInvalidPeerType", err)
	}
}
