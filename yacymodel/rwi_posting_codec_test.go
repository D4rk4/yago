package yacymodel

import (
	"errors"
	"maps"
	"testing"
)

func TestEncodeRWIPostingRoundTrip(t *testing.T) {
	entry, err := ParseRWIPosting(
		"ABCDEFGHIJKL{a=AAE,c=1,d=104,g=1,h=MNOPQRSTUVWX,l=en,s=AAI,t=258,x=2,z=AAAAAAA}",
	)
	if err != nil {
		t.Fatal(err)
	}

	encoded := EncodeRWIPosting(entry)
	decoded, err := DecodeRWIPosting(entry.WordHash, encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.WordHash != entry.WordHash {
		t.Errorf("WordHash = %q, want %q", decoded.WordHash, entry.WordHash)
	}
	if !maps.Equal(decoded.Properties, entry.Properties) {
		t.Errorf("Properties =\n %v\nwant\n %v", decoded.Properties, entry.Properties)
	}
}

func TestEncodeRWIPostingPreservesExtraColumns(t *testing.T) {
	entry := RWIPosting{
		WordHash: "ABCDEFGHIJKL",
		Properties: map[string]string{
			ColURLHash:   "MNOPQRSTUVWX",
			"unmapped":   "value",
			"another-ex": "42",
		},
	}

	decoded, err := DecodeRWIPosting(entry.WordHash, EncodeRWIPosting(entry))
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(decoded.Properties, entry.Properties) {
		t.Errorf("Properties =\n %v\nwant\n %v", decoded.Properties, entry.Properties)
	}
}

func TestEncodeRWIPostingOmitsWordHash(t *testing.T) {
	entry := RWIPosting{
		WordHash:   "ABCDEFGHIJKL",
		Properties: map[string]string{ColURLHash: "MNOPQRSTUVWX"},
	}

	encoded := EncodeRWIPosting(entry)
	decoded, err := DecodeRWIPosting("ZZZZZZZZZZZZ", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WordHash != "ZZZZZZZZZZZZ" {
		t.Errorf("WordHash = %q, want supplied key prefix", decoded.WordHash)
	}
}

func TestEncodeRWIPostingIsSmallerThanPropertyForm(t *testing.T) {
	entry, err := ParseRWIPosting(sampleRWILine)
	if err != nil {
		t.Fatal(err)
	}
	if got, legacy := len(EncodeRWIPosting(entry)), len(entry.String()); got >= legacy {
		t.Errorf("binary %d bytes, property form %d bytes", got, legacy)
	}
}

func TestDecodeRWIPostingFallsBackToPropertyForm(t *testing.T) {
	decoded, err := DecodeRWIPosting("ignored-word", []byte(sampleRWILine))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WordHash != "ABCDEFGHIJKL" {
		t.Errorf("WordHash = %q, want word hash parsed from property form", decoded.WordHash)
	}
	if decoded.Properties[ColHitCount] != "1" {
		t.Errorf("hit count = %q", decoded.Properties[ColHitCount])
	}
}

func TestDecodeRWIPostingRejectsEmptyValue(t *testing.T) {
	if _, err := DecodeRWIPosting("ABCDEFGHIJKL", nil); !errors.Is(err, ErrBadRWIPosting) {
		t.Errorf("err = %v, want ErrBadRWIPosting", err)
	}
}

func TestDecodeRWIPostingRejectsTruncatedBinary(t *testing.T) {
	entry, err := ParseRWIPosting(sampleRWILine)
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodeRWIPosting(entry)
	for length := 1; length < len(encoded); length++ {
		_, err := DecodeRWIPosting(entry.WordHash, encoded[:length])
		if !errors.Is(err, ErrBadRWIPosting) {
			t.Errorf("truncated to %d bytes: err = %v, want ErrBadRWIPosting", length, err)
		}
	}
}
