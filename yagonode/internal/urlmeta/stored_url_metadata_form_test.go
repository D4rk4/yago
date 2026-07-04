package urlmeta

import (
	"errors"
	"maps"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const sampleURLMetadataRow = "{flags=AAAAAA,fresh=20260101,hash=MNOPQRSTUVWX,load=20250101,mod=20250101,size=1024,url=b|aHR0cHM6Ly9leGFtcGxlLm9yZy8,wc=12}"

func TestEncodeStoredURLMetadataRoundTrip(t *testing.T) {
	row, err := yagomodel.ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}

	encoded := encodeStoredURLMetadata(row)
	decoded, err := decodeStoredURLMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(decoded.Properties, row.Properties) {
		t.Errorf("Properties =\n %v\nwant\n %v", decoded.Properties, row.Properties)
	}
}

func TestEncodeStoredURLMetadataIsSmallerThanPropertyForm(t *testing.T) {
	row, err := yagomodel.ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeStoredURLMetadata(row)
	if got, legacy := len(encoded), len(row.String()); got >= legacy {
		t.Errorf("compressed %d bytes, property form %d bytes", got, legacy)
	}
}

func TestDecodeStoredURLMetadataFallsBackToPropertyForm(t *testing.T) {
	decoded, err := decodeStoredURLMetadata([]byte(sampleURLMetadataRow))
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Properties[yagomodel.URLMetaHash]; got != "MNOPQRSTUVWX" {
		t.Errorf("hash = %q, want MNOPQRSTUVWX", got)
	}
}

func TestDecodeStoredURLMetadataRejectsEmptyValue(t *testing.T) {
	if _, err := decodeStoredURLMetadata(nil); !errors.Is(err, yagomodel.ErrBadURLMetadata) {
		t.Errorf("err = %v, want ErrBadURLMetadata", err)
	}
}

func TestDecodeStoredURLMetadataRejectsTruncatedCompressed(t *testing.T) {
	row, err := yagomodel.ParseURIMetadataRow(sampleURLMetadataRow)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeStoredURLMetadata(row)
	if _, err := decodeStoredURLMetadata(encoded[:len(encoded)-1]); err == nil {
		t.Error("expected error for truncated compressed value")
	}
}
