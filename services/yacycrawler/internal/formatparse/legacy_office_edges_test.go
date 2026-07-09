package formatparse

import (
	"encoding/binary"
	"strings"
	"testing"
)

// TestWordDocGuards covers the defensive exits of the Word extractor: a short
// or mis-signed WordDocument stream and a malformed CLX yield no text, and a
// missing table stream falls back to the WordDocument stream.
func TestWordDocGuards(t *testing.T) {
	short := buildCompoundFile([]cfbStream{{name: "WordDocument", data: []byte("tiny")}})
	if page, parsed := parseLegacyOffice("https://a.example/a.doc", short); parsed {
		t.Fatalf("short WordDocument parsed: %+v", page)
	}

	badSig := make([]byte, 0x210) // valid signature area but no FIB magic.
	body := buildCompoundFile([]cfbStream{{name: "WordDocument", data: badSig}})
	if _, parsed := parseLegacyOffice("https://a.example/b.doc", body); parsed {
		t.Fatal("WordDocument without the FIB magic must stay unparsed")
	}

	// Valid magic but a zero-length CLX: no pieces, nothing to extract.
	noClx := make([]byte, 0x200)
	binary.LittleEndian.PutUint16(noClx[0:], 0xA5EC)
	body = buildCompoundFile([]cfbStream{{name: "WordDocument", data: noClx}})
	if _, parsed := parseLegacyOffice("https://a.example/c.doc", body); parsed {
		t.Fatal("empty CLX must stay unparsed")
	}
}

// TestWordDocTableFallback puts the CLX inside the WordDocument stream itself
// so the missing table stream forces the fallback path, and points a piece
// past the stream end to exercise the out-of-range guard.
func TestWordDocTableFallback(t *testing.T) {
	const headerLen = 0x200
	text := []byte("Fallback body text")
	wordDoc := make([]byte, 0, headerLen+len(text)+64)
	wordDoc = append(wordDoc, make([]byte, headerLen)...)
	binary.LittleEndian.PutUint16(wordDoc[0:], 0xA5EC)
	textOffset := u32(len(wordDoc))
	wordDoc = append(wordDoc, text...)

	table := buildWordTable(textOffset, textOffset, u32(len(text)), 0)
	clxOffset := u32(len(wordDoc))
	wordDoc = append(wordDoc, table...)
	binary.LittleEndian.PutUint32(wordDoc[0x01A2:], clxOffset)
	binary.LittleEndian.PutUint32(wordDoc[0x01A6:], u32(len(table)))

	body := buildCompoundFile([]cfbStream{{name: "WordDocument", data: wordDoc}})
	page, parsed := parseLegacyOffice("https://a.example/fallback.doc", body)
	if !parsed || !strings.Contains(page.Text, "Fallback body text") {
		t.Fatalf("table fallback = %v %+v", parsed, page)
	}
}

// TestExcelAndPowerPointGuards cover the missing-stream exits.
func TestExcelAndPowerPointGuards(t *testing.T) {
	empty := buildCompoundFile([]cfbStream{{name: "Other", data: []byte("x")}})
	if _, parsed := parseLegacyOffice("https://a.example/a.xls", empty); parsed {
		t.Fatal("xls without a Workbook stream must stay unparsed")
	}
	if _, parsed := parseLegacyOffice("https://a.example/a.ppt", empty); parsed {
		t.Fatal("ppt without its document stream must stay unparsed")
	}

	// A Book (BIFF5) stream is the alternate spelling of Workbook.
	book := biffRecord0(0x00FC, func() []byte {
		var sst []byte
		sst = le32(sst, 1)
		sst = le32(sst, 1)

		return append(sst, xlString("BookCell", false, false)...)
	}())
	body := buildCompoundFile([]cfbStream{{name: "Book", data: book}})
	page, parsed := parseLegacyOffice("https://a.example/b.xls", body)
	if !parsed || !strings.Contains(page.Text, "BookCell") {
		t.Fatalf("Book stream = %v %+v", parsed, page)
	}
}

// TestODFDropsGeneratorNoise proves the metadata-noise elements (the authoring
// application and timestamps) are dropped while the title survives.
func TestODFDropsGeneratorNoise(t *testing.T) {
	body := zipBody(t, map[string]string{
		"content.xml": `<office:document-content><office:body><office:text>` +
			`<text:p>Real body sentence.</text:p></office:text></office:body>` +
			`</office:document-content>`,
		"meta.xml": `<office:document-meta><office:meta>` +
			`<meta:generator>MicrosoftOffice/14.0 MicrosoftWord</meta:generator>` +
			`<meta:creation-date>2012-07-03T22:21:00Z</meta:creation-date>` +
			`<dc:title>Kept Title</dc:title></office:meta></office:document-meta>`,
	})
	page, parsed := Parse(
		"https://a.example/doc.odt", "application/vnd.oasis.opendocument.text",
		body, DefaultToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Kept Title") ||
		!strings.Contains(page.Text, "Real body sentence.") {
		t.Fatalf("odt parse = %v %+v", parsed, page)
	}
	if strings.Contains(page.Text, "MicrosoftOffice") ||
		strings.Contains(page.Text, "2012-07-03") {
		t.Fatalf("metadata noise leaked: %q", page.Text)
	}
}
