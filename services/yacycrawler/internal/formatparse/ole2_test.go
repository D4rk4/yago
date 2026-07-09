package formatparse

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestCompoundFileRoundTrip proves the reader recovers streams from both the
// mini-stream path (small streams) and the regular FAT path (large streams),
// and that the test builder produces a container the reader accepts.
func TestCompoundFileRoundTrip(t *testing.T) {
	large := bytes.Repeat([]byte("LARGE-"), 1000) // > mini cutoff → regular FAT.
	body := buildCompoundFile([]cfbStream{
		{name: "Small", data: []byte("tiny stream body")},
		{name: "Empty", data: nil},
		{name: "Big", data: large},
	})
	file, err := openCompoundFile(body)
	if err != nil {
		t.Fatalf("open compound file: %v", err)
	}
	if got := file.stream("Small"); string(got) != "tiny stream body" {
		t.Fatalf("small stream = %q", got)
	}
	if got := file.stream("Empty"); len(got) != 0 {
		t.Fatalf("empty stream = %q", got)
	}
	if got := file.stream("Big"); !bytes.Equal(got, large) {
		t.Fatalf("large stream len = %d, want %d", len(got), len(large))
	}
	if got := file.stream("Absent"); got != nil {
		t.Fatalf("absent stream = %q", got)
	}
}

// TestCompoundFileRejectsNonContainers covers the reader's guards.
func TestCompoundFileRejectsNonContainers(t *testing.T) {
	if _, err := openCompoundFile([]byte("too short")); !errors.Is(err, errNotCompoundFile) {
		t.Fatalf("short body err = %v", err)
	}
	notCFB := make([]byte, cfbHeaderSize+16)
	if _, err := openCompoundFile(notCFB); !errors.Is(err, errNotCompoundFile) {
		t.Fatalf("zero signature err = %v", err)
	}
	badShift := make([]byte, cfbHeaderSize+16)
	copy(badShift, cfbSignature)
	// A zero sector shift yields an unsupported sector size.
	if _, err := openCompoundFile(badShift); !errors.Is(err, errCompoundCorrupt) {
		t.Fatalf("bad sector shift err = %v", err)
	}
}

// TestExtractCompoundRunsSkipsMetadata proves the Visio / unknown fallback
// pulls readable runs from content streams while ignoring the OLE machinery
// and metadata streams.
func TestExtractCompoundRunsSkipsMetadata(t *testing.T) {
	body := buildCompoundFile([]cfbStream{
		{name: "VisioDocument", data: []byte("Network diagram shapes and labels")},
		{name: "\x05SummaryInformation", data: []byte("GENERATORNOISE metadata blob")},
		{name: "CompObj", data: []byte("MicrosoftVisioMachineryString")},
	})
	page, parsed := parseLegacyOffice("https://a.example/net.vsd", body)
	if !parsed || !strings.Contains(page.Text, "Network diagram shapes and labels") {
		t.Fatalf("visio fallback = %v %+v", parsed, page)
	}
	if strings.Contains(page.Text, "GENERATORNOISE") ||
		strings.Contains(page.Text, "MicrosoftVisioMachineryString") {
		t.Fatalf("metadata leaked into text: %q", page.Text)
	}
}

// TestParseLegacyOfficeRejectsNonCompound keeps a non-container body unparsed.
func TestParseLegacyOfficeRejectsNonCompound(t *testing.T) {
	if _, parsed := parseLegacyOffice("https://a.example/x.doc", []byte("plain")); parsed {
		t.Fatal("a non-compound .doc must stay unparsed")
	}
}

// TestCompoundFileTruncated feeds progressively truncated containers through
// the reader, exercising the corruption guards: a missing FAT, directory, or
// data sector must fail cleanly or read short rather than panic.
func TestCompoundFileTruncated(t *testing.T) {
	large := bytes.Repeat([]byte("Z"), 5000) // multi-sector regular stream.
	full := buildCompoundFile([]cfbStream{
		{name: "Small", data: []byte("tiny")},
		{name: "Big", data: large},
	})
	for cut := len(full) - 512; cut >= cfbHeaderSize; cut -= 512 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("truncation to %d panicked: %v", cut, r)
				}
			}()
			file, err := openCompoundFile(full[:cut])
			if err != nil {
				return
			}
			// A reader that still opens must never over-read its backing slice.
			if got := file.stream("Big"); len(got) > len(large) {
				t.Fatalf("over-read at cut %d: %d bytes", cut, len(got))
			}
			_ = file.stream("Small")
		}()
	}
}

// TestCompoundFileMultiSectorStream reads back a stream spanning several
// regular sectors exactly, guarding the chain-assembly length bookkeeping.
func TestCompoundFileMultiSectorStream(t *testing.T) {
	body := buildCompoundFile([]cfbStream{{name: "Big", data: bytes.Repeat([]byte("Q"), 6000)}})
	file, err := openCompoundFile(body)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := file.stream("Big"); len(got) != 6000 {
		t.Fatalf("large stream length = %d", len(got))
	}
}
