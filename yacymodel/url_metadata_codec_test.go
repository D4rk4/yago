package yacymodel

import (
	"errors"
	"maps"
	"testing"
)

const sampleURLMetadataRow = "{flags=AAAAAA,fresh=20260101,hash=MNOPQRSTUVWX,load=20250101,mod=20250101,size=1024,url=b|aHR0cHM6Ly9leGFtcGxlLm9yZy8,wc=12}"

func TestEncodeURIMetadataRoundTrip(t *testing.T) {
	row, err := ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := EncodeURIMetadata(row)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeURIMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(decoded.Properties, row.Properties) {
		t.Errorf("Properties =\n %v\nwant\n %v", decoded.Properties, row.Properties)
	}
}

func TestEncodeURIMetadataIsSmallerThanPropertyForm(t *testing.T) {
	row, err := ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeURIMetadata(row)
	if err != nil {
		t.Fatal(err)
	}
	if got, legacy := len(encoded), len(row.String()); got >= legacy {
		t.Errorf("compressed %d bytes, property form %d bytes", got, legacy)
	}
}

func TestDecodeURIMetadataFallsBackToPropertyForm(t *testing.T) {
	decoded, err := DecodeURIMetadata([]byte(sampleURLMetadataRow))
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Properties[URLMetaHash]; got != "MNOPQRSTUVWX" {
		t.Errorf("hash = %q, want MNOPQRSTUVWX", got)
	}
}

func TestDecodeURIMetadataRejectsEmptyValue(t *testing.T) {
	if _, err := DecodeURIMetadata(nil); !errors.Is(err, ErrBadURLMetadata) {
		t.Errorf("err = %v, want ErrBadURLMetadata", err)
	}
}

func TestDecodeURIMetadataRejectsTruncatedCompressed(t *testing.T) {
	row, err := ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeURIMetadata(row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeURIMetadata(encoded[:len(encoded)-1]); err == nil {
		t.Error("expected error for truncated compressed value")
	}
}
