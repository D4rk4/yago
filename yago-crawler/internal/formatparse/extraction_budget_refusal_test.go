package formatparse

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// orderedZipBody writes the parts in the given order. The map-based zipBody
// helper is fine for content assertions but useless for a budget test: Go
// randomizes map iteration, so which entry lands before the cap would change
// run to run.
func orderedZipBody(t *testing.T, names []string, contents []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for index, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(contents[index])); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

// TestParseArchiveStopsExtractingPastTheTextBudget pins the extracted-text cap
// on the archive walk. One entry is already over the budget, so the entry after
// it must not be appended at all: without the cap a single archive of a hundred
// large text members would hand the indexer a hundred megabytes of page text,
// which is the archive-bomb shape the bounds exist to stop. The first entry
// still indexes, so the cap trims the walk rather than failing the page.
func TestParseArchiveStopsExtractingPastTheTextBudget(t *testing.T) {
	body := orderedZipBody(t,
		[]string{"a-first.txt", "z-past-budget.txt"},
		[]string{
			"BeforeBudgetMarker" + strings.Repeat("x", archiveMaxTextBytes),
			"PastBudgetMarker text body",
		},
	)

	page, parsed := Parse(
		"https://a.example/budget.zip", "application/zip", body, archiveToggles(),
	)
	if !parsed {
		t.Fatal("an archive whose first entry fills the budget must still index")
	}
	if !strings.Contains(page.Text, "BeforeBudgetMarker") {
		t.Fatal("the entry that fits must be extracted")
	}
	if strings.Contains(page.Text, "PastBudgetMarker") {
		t.Fatalf("entry content past the text budget was extracted: %d text bytes", len(page.Text))
	}
	if strings.Contains(page.Text, "z-past-budget.txt") {
		t.Fatalf("entry name past the text budget was extracted: %d text bytes", len(page.Text))
	}
}

// TestVorbisCommentLinesStopsAtCommentCap pins the Ogg comment cap. The comment
// count is attacker-controlled metadata: an honest file carries a handful of
// tags, so a stream declaring hundreds is either corrupt or crafted, and
// indexing all of them turns an audio tag reader into an arbitrary-length text
// injection channel. Comments up to the cap are still read.
func TestVorbisCommentLinesStopsAtCommentCap(t *testing.T) {
	comments := make([]string, 0, vorbisMaxComments+8)
	for index := range vorbisMaxComments + 8 {
		comments = append(comments, fmt.Sprintf("KEY%03d=value%03d", index, index))
	}

	lines := vorbisCommentLines(oggWithVorbisComments(comments))
	if len(lines) != vorbisMaxComments {
		t.Fatalf("vorbis comment lines = %d, want %d", len(lines), vorbisMaxComments)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "value000") {
		t.Fatal("comments within the cap must be read")
	}
	past := fmt.Sprintf("value%03d", vorbisMaxComments)
	if strings.Contains(strings.Join(lines, "\n"), past) {
		t.Fatalf("comment past the cap was read: %v", lines)
	}
}

func oggWithVorbisComments(comments []string) []byte {
	var ogg bytes.Buffer
	ogg.WriteString("OggS junk ")
	ogg.WriteByte(3)
	ogg.WriteString("vorbis")
	const vendor = "yago-test"
	_ = binary.Write(&ogg, binary.LittleEndian, len32(len(vendor)))
	ogg.WriteString(vendor)
	_ = binary.Write(&ogg, binary.LittleEndian, len32(len(comments)))
	for _, comment := range comments {
		_ = binary.Write(&ogg, binary.LittleEndian, len32(len(comment)))
		ogg.WriteString(comment)
	}

	return ogg.Bytes()
}

// TestPrintableRunsStopsAtRunCap pins the readable-run cap that bounds the MSG
// and wma/ra/rm readers. Both walk an undecoded container looking for letter
// runs, so every byte of a hostile file can be made to look like one more run:
// without the cap a megabyte of alternating letters and separators becomes a
// hundred thousand index lines. Runs up to the cap are still collected.
func TestPrintableRunsStopsAtRunCap(t *testing.T) {
	// 0x01 separates runs: it is not printable ASCII and is not the 0x00 that
	// the UTF-16LE detection would fold into the preceding letter.
	body := bytes.Repeat([]byte("word\x01"), msgMaxRuns+100)

	runs := printableRuns(body)
	if len(runs) != msgMaxRuns {
		t.Fatalf("printable runs = %d, want %d", len(runs), msgMaxRuns)
	}
	page, parsed := parseMSG("https://a.example/mail.msg", body)
	if !parsed {
		t.Fatal("a body with readable runs must parse")
	}
	if got := strings.Count(page.Text, "\n") + 1; got != msgMaxRuns {
		t.Fatalf("msg text lines = %d, want %d", got, msgMaxRuns)
	}
}

// TestParseOfficeStopsAtThePartCap pins the OOXML content-part cap. A slide
// deck's part names are container-controlled, so a crafted zip can declare
// thousands of ppt/slides/slideN.xml members and each one is read to
// officeMaxPartBytes before its text is appended. Parts up to the cap still
// index, so a real deck is not truncated.
func TestParseOfficeStopsAtThePartCap(t *testing.T) {
	names := make([]string, 0, officeMaxParts+6)
	contents := make([]string, 0, officeMaxParts+6)
	for index := 1; index <= officeMaxParts+6; index++ {
		names = append(names, fmt.Sprintf("ppt/slides/slide%03d.xml", index))
		contents = append(
			contents,
			fmt.Sprintf("<p:sld><a:t>SlideBody%03d</a:t></p:sld>", index),
		)
	}

	page, parsed := Parse(
		"https://a.example/deck.pptx",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		orderedZipBody(t, names, contents),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed {
		t.Fatal("a deck with more slides than the part cap must still index")
	}
	lastAdmitted := fmt.Sprintf("SlideBody%03d", officeMaxParts)
	if !strings.Contains(page.Text, lastAdmitted) {
		t.Fatalf("slide at the part cap was skipped: %q", page.Text)
	}
	firstRefused := fmt.Sprintf("SlideBody%03d", officeMaxParts+1)
	if strings.Contains(page.Text, firstRefused) {
		t.Fatalf("slide past the part cap was extracted: %d text bytes", len(page.Text))
	}
}

// TestSSTReaderAcceptsTheMaximumCharacterCount is the accepting half of the
// shared-string length guard: the oversized side is pinned by
// TestSSTReaderCharGuards, but a count of exactly sstMaxChars is a legal
// string and must be decoded from whatever the segments hold rather than
// discarded as corrupt.
func TestSSTReaderAcceptsTheMaximumCharacterCount(t *testing.T) {
	reader := &sstReader{segments: [][]byte{{0x41}}}
	if got := reader.readChars(sstMaxChars, false); got != "A" {
		t.Fatalf("cch at the cap = %q, want %q", got, "A")
	}
}
