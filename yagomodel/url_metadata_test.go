package yagomodel

import (
	"context"
	"errors"
	"testing"
)

func TestParseURIMetadataRowRoundTrip(t *testing.T) {
	row := "{flags=AAAAAA,fresh=20260101,hash=MNOPQRSTUVWX,load=20250101,mod=20250101,size=1024,url=b|aHR0cHM6Ly9leGFtcGxlLm9yZy8,wc=12}"
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

func TestParseURIMetadataRowEmptyFlags(t *testing.T) {
	row := "{flags=,hash=MNOPQRSTUVWX}"
	parsed, err := ParseURIMetadataRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.String(); got != row {
		t.Errorf("round trip:\n got %q\nwant %q", got, row)
	}
}

func TestParseURIMetadataRowShortFlags(t *testing.T) {
	row := "{flags=AAAA,hash=MNOPQRSTUVWX}"
	parsed, err := ParseURIMetadataRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.String(); got != row {
		t.Errorf("round trip:\n got %q\nwant %q", got, row)
	}
}

func TestURLHashFallback(t *testing.T) {
	parsed, err := ParseURIMetadataRow("{h=MNOPQRSTUVWX,url=b|aHR0cHM6Ly9leGFtcGxlLm9yZy8}")
	if err != nil {
		t.Fatal(err)
	}
	h, err := parsed.URLHash()
	if err != nil || h != "MNOPQRSTUVWX" {
		t.Errorf("URLHash() fallback = %q, %v", h, err)
	}
}

func TestURLHashMissing(t *testing.T) {
	if _, err := ParseURIMetadataRow(
		"{url=b|aHR0cHM6Ly9leGFtcGxlLm9yZy8}",
	); !errors.Is(
		err,
		ErrBadURLMetadata,
	) {
		t.Fatalf("ParseURIMetadataRow() = %v, want ErrBadURLMetadata", err)
	}
}

func TestParseURIMetadataRowErrors(t *testing.T) {
	for _, bad := range []string{
		"",
		"hash=MNOPQRSTUVWX",
		"{}",
		"{=novalue}",
		"{hash=MNOPQRSTUVWX,badtoken}",
		"{hash=short}",
		"{h=short}",
		"{hash=MNOPQRSTUVWX,referrer=short}",
		"{hash=MNOPQRSTUVWX,flags=!}",
		"{hash=MNOPQRSTUVWX,dt=}",
		"{hash=MNOPQRSTUVWX,size=bad}",
	} {
		if _, err := ParseURIMetadataRow(bad); !errors.Is(err, ErrBadURLMetadata) {
			t.Errorf("ParseURIMetadataRow(%q) = %v, want ErrBadURLMetadata", bad, err)
		}
	}
}

func TestParseURIMetadataRowSkipsEmptyPropertyTokens(t *testing.T) {
	row, err := ParseURIMetadataRow("{hash=MNOPQRSTUVWX,}")
	if err != nil {
		t.Fatal(err)
	}
	if got := row.Properties[URLMetaHash]; got != "MNOPQRSTUVWX" {
		t.Fatalf("hash = %q", got)
	}
}

func TestTitleDecodesDescription(t *testing.T) {
	const title = "Quarterly Earnings Report"
	row := URIMetadataRow{Properties: map[string]string{
		URLMetaColDescription: EncodeBase64WireForm(title),
	}}

	got, err := row.Title(context.Background())
	if err != nil {
		t.Fatalf("Title: %v", err)
	}
	if got != title {
		t.Fatalf("title = %q, want %q", got, title)
	}
}

func TestTitleEmptyWhenAbsent(t *testing.T) {
	got, err := URIMetadataRow{Properties: map[string]string{}}.Title(context.Background())
	if err != nil {
		t.Fatalf("Title: %v", err)
	}
	if got != "" {
		t.Fatalf("title = %q, want empty", got)
	}
}

func TestTitleRejectsCorruptDescription(t *testing.T) {
	row := URIMetadataRow{Properties: map[string]string{URLMetaColDescription: "z|@@@"}}
	if _, err := row.Title(context.Background()); err == nil {
		t.Fatal("corrupt description should fail")
	}
}
