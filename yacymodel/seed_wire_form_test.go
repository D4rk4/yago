package yacymodel

import (
	"errors"
	"strings"
	"testing"
)

func TestSeedWireFormRoundTrip(t *testing.T) {
	for _, seed := range []string{
		sampleSeed,
		"",
		strings.Repeat("Key=Value,", 100),
	} {
		got, err := DecodeSeedWireForm(EncodeSeedWireForm(seed))
		if err != nil {
			t.Fatalf("round trip %q: %v", seed, err)
		}
		if got != seed {
			t.Errorf("round trip:\n got %q\nwant %q", got, seed)
		}
	}
}

func TestEncodeSeedWireFormPicksShortest(t *testing.T) {
	short := EncodeSeedWireForm("Hash=ABCDEFGHIJKL")
	if short[0] != wireFormPlain {
		t.Errorf("short seed should stay plain, got tag %q", short[0])
	}
	long := EncodeSeedWireForm(strings.Repeat("Key=Value,", 200))
	if long[0] != wireFormGzip {
		t.Errorf("highly compressible seed should gzip, got tag %q", long[0])
	}
}

func TestDecodeSeedWireFormExplicit(t *testing.T) {
	plain, err := DecodeSeedWireForm("p|Hash=ABCDEFGHIJKL")
	if err != nil || plain != "Hash=ABCDEFGHIJKL" {
		t.Errorf("plain decode = %q, %v", plain, err)
	}
	b64, err := DecodeSeedWireForm("b|" + Encode([]byte("{Hash=ABCDEFGHIJKL}")))
	if err != nil || b64 != "{Hash=ABCDEFGHIJKL}" {
		t.Errorf("b64 decode = %q, %v", b64, err)
	}
}

func TestDecodeSeedWireFormErrors(t *testing.T) {
	for _, bad := range []string{"", "x", "q|data", "b|==="} {
		if _, err := DecodeSeedWireForm(bad); err == nil {
			t.Errorf("DecodeSeedWireForm(%q) = nil error", bad)
		}
	}
	if _, err := DecodeSeedWireForm("q|data"); !errors.Is(err, ErrBadSeedWireForm) {
		t.Errorf("unknown tag should be ErrBadSeedWireForm")
	}
}
