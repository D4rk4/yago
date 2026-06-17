package yacymodel

import (
	"errors"
	"testing"
)

func TestParseURIMetadataRowRoundTrip(t *testing.T) {
	row := "hash=MNOPQRSTUVWX,size=1024,title=Example"
	parsed, err := ParseURIMetadataRow(row)
	if err != nil {
		t.Fatal(err)
	}
	h, err := parsed.URLHash()
	if err != nil || h != "MNOPQRSTUVWX" {
		t.Errorf("URLHash() = %q, %v", h, err)
	}
	if got := parsed.String(); got != row {
		t.Errorf("round trip:\n got %q\nwant %q", got, row)
	}
}

func TestURLHashFallback(t *testing.T) {
	parsed, err := ParseURIMetadataRow("h=MNOPQRSTUVWX,title=Example")
	if err != nil {
		t.Fatal(err)
	}
	h, err := parsed.URLHash()
	if err != nil || h != "MNOPQRSTUVWX" {
		t.Errorf("URLHash() fallback = %q, %v", h, err)
	}
}

func TestURLHashMissing(t *testing.T) {
	parsed, err := ParseURIMetadataRow("title=Example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.URLHash(); !errors.Is(err, ErrBadURLMetadata) {
		t.Fatalf("URLHash() = %v, want ErrBadURLMetadata", err)
	}
}

func TestParseURIMetadataRowErrors(t *testing.T) {
	for _, bad := range []string{"", "=novalue"} {
		if _, err := ParseURIMetadataRow(bad); !errors.Is(err, ErrBadURLMetadata) {
			t.Errorf("ParseURIMetadataRow(%q) = %v, want ErrBadURLMetadata", bad, err)
		}
	}
}
