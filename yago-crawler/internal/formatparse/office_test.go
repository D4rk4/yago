package formatparse

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func zipBody(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range parts {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

func TestParseOOXMLWord(t *testing.T) {
	body := zipBody(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="ns"><w:body>` +
			`<w:p><w:r><w:t>First paragraph.</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Second paragraph.</w:t></w:r></w:p></w:body></w:document>`,
		"word/styles.xml": `<w:styles xmlns:w="ns"><w:style>IgnoredStyle</w:style></w:styles>`,
	})
	page, parsed := Parse(
		"https://a.example/report.docx",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		body, yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "First paragraph.") ||
		!strings.Contains(page.Text, "Second paragraph.") {
		t.Fatalf("docx parse = %v %+v", parsed, page)
	}
	if strings.Contains(page.Text, "IgnoredStyle") {
		t.Fatal("non-content parts must not index")
	}
	if !strings.Contains(page.Text, "First paragraph. \n") {
		t.Fatalf("paragraph break missing: %q", page.Text)
	}
}

func TestParseOOXMLSheetAndSlides(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	sheet := zipBody(t, map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>Revenue</t></si><si><t>Cost</t></si></sst>`,
	})
	page, parsed := Parse("https://a.example/book.xlsx", "application/zip", sheet, toggles)
	if !parsed || !strings.Contains(page.Text, "Revenue") || !strings.Contains(page.Text, "Cost") {
		t.Fatalf("xlsx parse = %v %+v", parsed, page)
	}

	slides := zipBody(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld><a:t>Slide one title</a:t></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld><a:t>Slide two body</a:t></p:sld>`,
	})
	page, parsed = Parse("https://a.example/deck.pptx", "application/zip", slides, toggles)
	if !parsed || !strings.Contains(page.Text, "Slide one title") ||
		!strings.Contains(page.Text, "Slide two body") {
		t.Fatalf("pptx parse = %v %+v", parsed, page)
	}
}

func TestParseODFText(t *testing.T) {
	body := zipBody(t, map[string]string{
		"content.xml": `<office:document-content><office:body><office:text>` +
			`<text:p>Opening line.</text:p><text:p>Closing line.</text:p>` +
			`</office:text></office:body></office:document-content>`,
		"meta.xml": `<office:document-meta><dc:title>Odt title</dc:title></office:document-meta>`,
	})
	for _, target := range []string{
		"https://a.example/doc.odt", "https://a.example/old.sxw",
	} {
		page, parsed := Parse(
			target, "application/vnd.oasis.opendocument.text", body,
			yagocrawlcontract.DefaultFormatToggles(),
		)
		if !parsed || !strings.Contains(page.Text, "Opening line.") ||
			!strings.Contains(page.Text, "Odt title") {
			t.Fatalf("%s parse = %v %+v", target, parsed, page)
		}
	}
}

func TestParseFreeMindAndLegacy(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	mind := `<map version="1.0"><node TEXT="Root topic">` +
		`<node TEXT="Child one"/><node TEXT=""><node TEXT="Grandchild"/></node></node></map>`
	page, parsed := Parse("https://a.example/map.mm", "text/xml", []byte(mind), toggles)
	if !parsed || page.Title != "Root topic" ||
		!strings.Contains(page.Text, "Child one") || !strings.Contains(page.Text, "Grandchild") {
		t.Fatalf("mm parse = %v %+v", parsed, page)
	}

	// A truncated compound-file signature is not a real .doc and must not be
	// mistaken for one — proper extraction needs the OLE2 directory structure.
	legacy := append([]byte{0xd0, 0xcf, 0x11, 0xe0}, []byte("Legacy Word body text here")...)
	if _, parsed := Parse(
		"https://a.example/old.doc",
		"application/msword",
		legacy,
		toggles,
	); parsed {
		t.Fatal("a truncated compound file must stay unparsed")
	}

	if _, parsed := Parse(
		"https://a.example/broken.docx", "application/zip", []byte("not a zip"), toggles,
	); parsed {
		t.Fatal("broken container must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/empty.odt", "application/zip",
		zipBody(t, map[string]string{"content.xml": `<a/>`}), toggles,
	); parsed {
		t.Fatal("text-free container must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/badmap.mm", "text/xml", []byte("<map"), toggles,
	); parsed {
		t.Fatal("malformed mind map must stay unparsed")
	}
	if _, parsed := parseOffice("https://a.example/none.unknown", "", nil); parsed {
		t.Fatal("non-office extension must stay unparsed")
	}
}

func TestOfficeAndDispatchEdges(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()

	// A bare-path URL with a family MIME routes through the MIME pass.
	if _, parsed := Parse(
		"https://a.example/download", "application/pdf", []byte("%PDF"), toggles,
	); parsed {
		t.Fatal("pdf family has no parser yet; MIME routing must still classify it")
	}

	// An empty mind map yields no text.
	if _, parsed := Parse(
		"https://a.example/void.mm", "text/xml",
		[]byte(`<map version="1.0"><node TEXT=""/></map>`), toggles,
	); parsed {
		t.Fatal("text-free mind map must stay unparsed")
	}

	// A container whose local entry header is corrupt skips that part.
	body := zipBody(t, map[string]string{
		"content.xml": `<text:p>Recoverable text</text:p>`,
	})
	corrupt := bytes.Replace(body, []byte("PK\x03\x04"), []byte("XXXX"), 1)
	if _, parsed := Parse(
		"https://a.example/corrupt.odt", "application/zip", corrupt, toggles,
	); parsed {
		t.Fatal("container with only corrupt parts must stay unparsed")
	}
}

func TestParseOfficeExtensionlessByMIME(t *testing.T) {
	docx := zipBody(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="ns"><w:body>` +
			`<w:p><w:r><w:t>Extension-less docx body.</w:t></w:r></w:p></w:body></w:document>`,
	})
	odt := zipBody(t, map[string]string{
		"content.xml": `<office:document-content><office:body><office:text>` +
			`<text:p>Extension-less odt body.</text:p></office:text></office:body></office:document-content>`,
	})
	legacy := buildCompoundFile([]cfbStream{
		{name: "WordDocument", data: []byte("Extension-less legacy office body.")},
	})
	cases := []struct {
		name        string
		contentType string
		body        []byte
		want        string
	}{
		{
			"ooxml",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			docx,
			"Extension-less docx body.",
		},
		{"odf", "application/vnd.oasis.opendocument.text", odt, "Extension-less odt body."},
		{"legacy", "application/msword", legacy, "Extension-less legacy office body."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, parsed := parseOffice("https://a.example/download", tc.contentType, tc.body)
			if !parsed || !strings.Contains(page.Text, tc.want) {
				t.Fatalf("parseOffice(%s) = %v %q", tc.name, parsed, page.Text)
			}
		})
	}
}

func TestParseOfficeExtensionlessByMagic(t *testing.T) {
	docx := zipBody(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="ns"><w:body>` +
			`<w:p><w:r><w:t>Magic-routed docx.</w:t></w:r></w:p></w:body></w:document>`,
	})
	odt := zipBody(t, map[string]string{
		"content.xml": `<office:document-content><office:body><office:text>` +
			`<text:p>Magic-routed odt.</text:p></office:text></office:body></office:document-content>`,
	})
	legacy := buildCompoundFile([]cfbStream{
		{name: "WordDocument", data: []byte("Magic-routed legacy office.")},
	})
	if page, ok := parseOffice("https://a.example/blob", "application/octet-stream", legacy); !ok ||
		!strings.Contains(page.Text, "Magic-routed legacy office.") {
		t.Fatalf("ole2 magic legacy = %v %q", ok, page.Text)
	}
	if page, ok := parseOffice("https://a.example/blob", "", docx); !ok ||
		!strings.Contains(page.Text, "Magic-routed docx.") {
		t.Fatalf("zip magic ooxml = %v %q", ok, page.Text)
	}
	if page, ok := parseOffice("https://a.example/blob", "", odt); !ok ||
		!strings.Contains(page.Text, "Magic-routed odt.") {
		t.Fatalf("zip magic odf = %v %q", ok, page.Text)
	}
	if _, ok := parseOffice("https://a.example/blob", "", []byte("not an office file")); ok {
		t.Fatal("non-office bytes must stay unparsed")
	}
}

func TestParseExtensionlessOfficeRoutesThroughRegistry(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	docx := zipBody(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="ns"><w:body>` +
			`<w:p><w:r><w:t>Routed through the registry.</w:t></w:r></w:p></w:body></w:document>`,
	})
	page, parsed := Parse(
		"https://a.example/download",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		docx, toggles,
	)
	if !parsed || !strings.Contains(page.Text, "Routed through the registry.") {
		t.Fatalf("registry routing = %v %q", parsed, page.Text)
	}
}
