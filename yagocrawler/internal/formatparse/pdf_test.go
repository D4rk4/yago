package formatparse

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func pdfWithText(t *testing.T, title, content string) []byte {
	t.Helper()
	var z bytes.Buffer
	writer := zlib.NewWriter(&z)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n1 0 obj\n<< /Title (" + title + ") >>\nendobj\n")
	pdf.WriteString("2 0 obj\n<< /Filter /FlateDecode >>\nstream\r\n")
	pdf.Write(z.Bytes())
	pdf.WriteString("\nendstream\nendobj\n%%EOF")

	return pdf.Bytes()
}

func TestParsePDFExtractsShownText(t *testing.T) {
	content := `BT /F1 12 Tf 72 700 Td (Hello searchable world) Tj T* ` +
		`[(Second) -250 (segment \(nested\) here)] TJ ET junk (outside block)`
	body := pdfWithText(t, "Doc title", content)
	page, parsed := Parse(
		"https://a.example/report.pdf", "application/pdf", body,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Doc title" {
		t.Fatalf("pdf parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"Hello searchable world", "Second", "segment (nested) here",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("pdf missing %q in %q", want, page.Text)
		}
	}
	if strings.Contains(page.Text, "outside block") {
		t.Fatal("literals outside BT..ET must not index")
	}
}

func TestParsePDFRejectsGarbageAndMissingText(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	if _, parsed := Parse(
		"https://a.example/fake.pdf", "application/pdf", []byte("not a pdf"), toggles,
	); parsed {
		t.Fatal("non-pdf body must stay unparsed")
	}
	// CID-like garbage: a text block whose literals hold no letters.
	body := pdfWithText(t, "", `BT [(\001\002\003) (\004\005)] TJ ET`)
	if _, parsed := Parse("https://a.example/cid.pdf", "application/pdf", body, toggles); parsed {
		t.Fatal("letterless pdf text must stay unparsed")
	}
	// A PDF whose stream is not flate-compressed yields nothing.
	raw := []byte("%PDF-1.4\nstream\nBT (Plain) Tj ET\nendstream\n%%EOF")
	if _, parsed := Parse(
		"https://a.example/rawstream.pdf",
		"application/pdf",
		raw,
		toggles,
	); parsed {
		t.Fatal("non-flate stream must yield no text")
	}
	// A stream without a terminator ends the walk.
	unterminated := []byte("%PDF-1.4\nstream\nabcdef")
	if streams := pdfContentStreams(unterminated); len(streams) != 0 {
		t.Fatalf("unterminated stream = %d", len(streams))
	}
	// A title without a nearby literal is ignored.
	noParen := []byte("%PDF-1.4\n/Title /Ref\n")
	if got := pdfInfoTitle(noParen); got != "" {
		t.Fatalf("titleless info = %q", got)
	}
	if got := pdfInfoTitle([]byte("%PDF-1.4\n")); got != "" {
		t.Fatalf("infoless pdf title = %q", got)
	}
	// A title falls back to the first text line when Info is absent.
	body = pdfWithText(t, "", "BT (Fallback title line) Tj ET")
	page, parsed := Parse("https://a.example/untitled.pdf", "application/pdf", body, toggles)
	if !parsed || page.Title != "Fallback title line" {
		t.Fatalf("fallback title = %v %+v", parsed, page)
	}
	// An unterminated BT block ends the content walk.
	if got := pdfTextFromContent([]byte("BT (never closed)")); got != "" {
		t.Fatalf("unterminated BT = %q", got)
	}
}

func TestParsePostScript(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	ps := "%!PS-Adobe-3.0\n/Helvetica findfont 12 scalefont setfont\n" +
		"72 700 moveto (PostScript shown text) show\n(Another line) show\nshowpage\n"
	page, parsed := Parse("https://a.example/doc.ps", "application/postscript", []byte(ps), toggles)
	if !parsed || !strings.Contains(page.Text, "PostScript shown text") ||
		!strings.Contains(page.Text, "Another line") {
		t.Fatalf("ps parse = %v %+v", parsed, page)
	}

	if _, parsed := Parse(
		"https://a.example/fake.ps", "application/postscript", []byte("binary"), toggles,
	); parsed {
		t.Fatal("non-ps body must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/empty.ps", "application/postscript", []byte("%!PS\nshowpage\n"), toggles,
	); parsed {
		t.Fatal("textless ps must stay unparsed")
	}

	// A PDF-extension body carrying PostScript routes to the PS extractor.
	if page, parsed := parsePDF(
		"https://a.example/mislabeled.pdf", "",
		[]byte("%!PS\n(Routed as postscript literal text) show\n"),
	); !parsed || !strings.Contains(page.Text, "Routed as postscript") {
		t.Fatalf("ps routing = %v %+v", parsed, page)
	}
}

func TestPDFLiteralEdges(t *testing.T) {
	if got := pdfEscape('n'); got != ' ' {
		t.Fatalf("escape n = %q", got)
	}
	if got, _ := pdfStringLiteral(
		[]byte("(trailing escape \\"),
	); !strings.HasPrefix(
		got,
		"trailing escape",
	) {
		t.Fatalf("trailing escape = %q", got)
	}
	if got, consumed := pdfStringLiteral(
		[]byte("(unterminated literal"),
	); consumed != len("(unterminated literal") ||
		got == "" {
		t.Fatalf("unterminated literal = %q %d", got, consumed)
	}
	if !hasIndexableText("enough letters here") || hasIndexableText("123 456") {
		t.Fatal("letter gate broken")
	}
}

func TestPDFBoundsAndNesting(t *testing.T) {
	// Text-cap break: a stream expanding beyond the text cap stops the walk.
	big := "BT (" + strings.Repeat("aaaa bbbb ", pdfMaxTextBytes/9) + ") Tj ET"
	body := pdfWithText(t, "", big)
	if _, parsed := Parse(
		"https://a.example/huge.pdf", "application/pdf", body,
		yagocrawlcontract.DefaultFormatToggles(),
	); !parsed {
		t.Fatal("huge pdf must still parse")
	}

	// A zlib stream that inflates to nothing skips.
	var z bytes.Buffer
	writer := zlib.NewWriter(&z)
	_ = writer.Close()
	empty := []byte("%PDF-1.4\nstream\n")
	empty = append(empty, z.Bytes()...)
	empty = append(empty, []byte("\nendstream")...)
	if streams := pdfContentStreams(empty); len(streams) != 0 {
		t.Fatalf("empty inflate = %d", len(streams))
	}

	// Raw nested parentheses inside a literal keep their text.
	if got, _ := pdfStringLiteral([]byte("(outer (inner) tail)")); got != "outer (inner) tail" {
		t.Fatalf("nested literal = %q", got)
	}
}

// buildLegacyPDF assembles a minimal PDF whose content stream is encoded
// with the 1990s ASCII85Decode+LZWDecode chain — the combination real PDF
// 1.0 archives use (CRAWL-17 follow-up).
func buildLegacyPDF(t *testing.T, content string) []byte {
	t.Helper()
	var lzwBuf bytes.Buffer
	lzwWriter := lzw.NewWriter(&lzwBuf, lzw.MSB, 8)
	if _, err := lzwWriter.Write([]byte(content)); err != nil {
		t.Fatalf("lzw write: %v", err)
	}
	_ = lzwWriter.Close()
	var a85 bytes.Buffer
	encoder := ascii85.NewEncoder(&a85)
	if _, err := encoder.Write(lzwBuf.Bytes()); err != nil {
		t.Fatalf("ascii85 write: %v", err)
	}
	_ = encoder.Close()

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.0\n")
	pdf.WriteString("6 0 obj\n<< /Length 99 /Filter [ /ASCII85Decode /LZWDecode ] >>\nstream\n")
	pdf.Write(a85.Bytes())
	pdf.WriteString("~>\nendstream\nendobj\n%%EOF")

	return pdf.Bytes()
}

// TestParsePDFDecodesLegacyFilterChains pins the multi-filter decoder: a
// PDF 1.0 file with ASCII85+LZW content parses, and an ASCIIHex stream does
// too, while an image-only filter chain stays unparsed.
func TestParsePDFDecodesLegacyFilterChains(t *testing.T) {
	content := "BT (Navy Public Service Award announcement) Tj ET"
	page, parsed := parsePDF("http://a.example/legacy.pdf", "application/pdf",
		buildLegacyPDF(t, content))
	if !parsed || !strings.Contains(page.Text, "Navy Public Service Award") {
		t.Fatalf("legacy pdf: parsed=%v text=%q", parsed, page.Text)
	}

	hexContent := hex.EncodeToString([]byte("BT (hex encoded body text here) Tj ET"))
	var hexPDF bytes.Buffer
	hexPDF.WriteString("%PDF-1.2\n<< /Filter /ASCIIHexDecode >>\nstream\n")
	hexPDF.WriteString(hexContent + ">")
	hexPDF.WriteString("\nendstream\n%%EOF")
	hexPage, hexParsed := parsePDF("http://a.example/hex.pdf", "application/pdf", hexPDF.Bytes())
	if !hexParsed || !strings.Contains(hexPage.Text, "hex encoded body") {
		t.Fatalf("hex pdf: parsed=%v text=%q", hexParsed, hexPage.Text)
	}

	var imagePDF bytes.Buffer
	imagePDF.WriteString("%PDF-1.4\n<< /Filter /DCTDecode >>\nstream\n")
	imagePDF.WriteString("\xff\xd8\xff jpeg bytes")
	imagePDF.WriteString("\nendstream\n%%EOF")
	if _, imageParsed := parsePDF(
		"http://a.example/scan.pdf", "application/pdf", imagePDF.Bytes(),
	); imageParsed {
		t.Fatal("image-only pdf must stay unparsed")
	}
}

// TestPDFFilterChain pins /Filter parsing directly: a missing or empty entry
// falls back to FlateDecode, a bare name reads as one filter, and a bracketed
// array reads the chain in order whether the bracket sits against the keyword
// or after whitespace.
func TestPDFFilterChain(t *testing.T) {
	cases := []struct {
		name string
		dict string
		want []string
	}{
		{"missing filter entry", "<< /Length 5 >>", []string{"FlateDecode"}},
		{"empty filter entry", "/Filter", []string{"FlateDecode"}},
		{"single bare name", "/Filter /FlateDecode", []string{"FlateDecode"}},
		{
			"bracket against keyword",
			"/Filter[/ASCII85Decode/LZWDecode]",
			[]string{"ASCII85Decode", "LZWDecode"},
		},
		{"bracket after spaces", "/Filter  [ /LZWDecode ]", []string{"LZWDecode"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := pdfFilterChain([]byte(testCase.dict))
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Fatalf("pdfFilterChain(%q) = %v, want %v", testCase.dict, got, testCase.want)
			}
		})
	}
}

// TestPDFDecodeHelperEdges pins each stream decoder's failure and edge paths:
// a corrupt filter payload aborts the stream (nonnil error) rather than
// emitting garbage, and an odd-length ASCIIHex run pads its final nibble.
func TestPDFDecodeHelperEdges(t *testing.T) {
	if _, err := pdfInflate([]byte("plainly not a zlib stream")); err == nil {
		t.Fatal("bad zlib header must error")
	}
	if _, err := pdfInflate([]byte{0x78, 0x9c, 0x00}); err == nil {
		t.Fatal("truncated deflate body must error with no output")
	}
	if _, err := pdfASCII85([]byte("vwxy")); err == nil {
		t.Fatal("invalid ascii85 characters must error")
	}
	if _, err := pdfLZW([]byte{0xff, 0xff}); err == nil {
		t.Fatal("invalid lzw code must error")
	}
	if _, err := pdfASCIIHex([]byte("zz")); err == nil {
		t.Fatal("non-hex ascii must error")
	}
	got, err := pdfASCIIHex([]byte("41 4"))
	if err != nil {
		t.Fatalf("odd-length hex must pad and decode: %v", err)
	}
	if len(got) != 2 || got[0] != 0x41 || got[1] != 0x40 {
		t.Fatalf("odd-length hex decode = % x, want 41 40", got)
	}
}
