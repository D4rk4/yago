package rwi

import (
	"errors"
	"maps"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const sampleRWILine = "ABCDEFGHIJKL{c=1,h=MNOPQRSTUVWX,x=2,z=AAAAAA}"

func TestEncodeStoredPostingRoundTrip(t *testing.T) {
	entry, err := yagomodel.ParseRWIPosting(
		"ABCDEFGHIJKL{a=AAE,c=1,d=104,g=1,h=MNOPQRSTUVWX,l=en,s=AAI,t=258,x=2,z=AAAAAAA}",
	)
	if err != nil {
		t.Fatal(err)
	}

	encoded := encodeStoredPosting(entry)
	decoded, err := decodeStoredPosting(entry.WordHash, encoded)
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

func TestEncodeStoredPostingPreservesExtraColumns(t *testing.T) {
	entry := yagomodel.RWIPosting{
		WordHash: "ABCDEFGHIJKL",
		Properties: map[string]string{
			yagomodel.ColURLHash: "MNOPQRSTUVWX",
			"unmapped":           "value",
			"another-ex":         "42",
		},
	}

	decoded, err := decodeStoredPosting(entry.WordHash, encodeStoredPosting(entry))
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(decoded.Properties, entry.Properties) {
		t.Errorf("Properties =\n %v\nwant\n %v", decoded.Properties, entry.Properties)
	}
}

func TestEncodeStoredPostingOmitsWordHash(t *testing.T) {
	entry := yagomodel.RWIPosting{
		WordHash:   "ABCDEFGHIJKL",
		Properties: map[string]string{yagomodel.ColURLHash: "MNOPQRSTUVWX"},
	}

	encoded := encodeStoredPosting(entry)
	decoded, err := decodeStoredPosting("ZZZZZZZZZZZZ", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WordHash != "ZZZZZZZZZZZZ" {
		t.Errorf("WordHash = %q, want supplied key prefix", decoded.WordHash)
	}
}

func TestEncodeStoredPostingIsSmallerThanPropertyForm(t *testing.T) {
	entry, err := yagomodel.ParseRWIPosting(sampleRWILine)
	if err != nil {
		t.Fatal(err)
	}
	if got, legacy := len(encodeStoredPosting(entry)), len(entry.String()); got >= legacy {
		t.Errorf("binary %d bytes, property form %d bytes", got, legacy)
	}
}

func TestDecodeStoredPostingFallsBackToPropertyForm(t *testing.T) {
	decoded, err := decodeStoredPosting("ignored-word", []byte(sampleRWILine))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WordHash != "ABCDEFGHIJKL" {
		t.Errorf("WordHash = %q, want word hash parsed from property form", decoded.WordHash)
	}
	if decoded.Properties[yagomodel.ColHitCount] != "1" {
		t.Errorf("hit count = %q", decoded.Properties[yagomodel.ColHitCount])
	}
}

func TestDecodeStoredPostingRejectsEmptyValue(t *testing.T) {
	if _, err := decodeStoredPosting(
		"ABCDEFGHIJKL",
		nil,
	); !errors.Is(
		err,
		yagomodel.ErrBadRWIPosting,
	) {
		t.Errorf("err = %v, want ErrBadRWIPosting", err)
	}
}

func TestDecodeStoredPostingRejectsTruncatedBinary(t *testing.T) {
	entry, err := yagomodel.ParseRWIPosting(sampleRWILine)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeStoredPosting(entry)
	for length := 1; length < len(encoded); length++ {
		_, err := decodeStoredPosting(entry.WordHash, encoded[:length])
		if !errors.Is(err, yagomodel.ErrBadRWIPosting) {
			t.Errorf("truncated to %d bytes: err = %v, want ErrBadRWIPosting", length, err)
		}
	}
}
