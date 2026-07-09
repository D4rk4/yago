package formatparse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

const (
	officeMaxPartBytes = 32 << 20
	officeMaxParts     = 64
)

// officeExtension classifies which container the office family member uses.
var (
	ooxmlExtensions = set("docx", "dotx", "pptx", "ppsx", "potx", "xlsx", "xltx")
	odfExtensions   = set(
		"odt", "ods", "odp", "odg", "odc", "odf", "odb", "odi", "odm",
		"ott", "ots", "otp", "otg",
	)
	legacyOfficeExtensions = set(
		"doc",
		"xls",
		"xla",
		"ppt",
		"pps",
		"vsd",
		"vss",
		"vst",
		"sxw",
		"sxc",
	)
)

// parseOffice handles the office family: OOXML and OpenDocument are zip+XML
// containers whose content parts yield the document text; FreeMind .mm is
// plain XML; the legacy binary formats (doc/xls/ppt/Visio) are OLE2 compound
// files decoded from their own text streams. Old StarOffice sxw/sxc use the
// ODF container too, so they route with it.
func parseOffice(rawURL, _ string, body []byte) (Document, bool) {
	ext := urlExtension(rawURL)
	switch {
	case ooxmlExtensions[ext]:
		return parseZipXMLText(rawURL, body, ooxmlContentPart)
	case odfExtensions[ext] || ext == "sxw" || ext == "sxc":
		return parseZipXMLText(rawURL, body, odfContentPart)
	case ext == "mm":
		return parseFreeMind(rawURL, body)
	case legacyOfficeExtensions[ext]:
		return parseLegacyOffice(rawURL, body)
	default:
		return Document{URL: rawURL}, false
	}
}

// ooxmlContentPart selects the OOXML parts carrying document text: the Word
// body, PowerPoint slides, and the Excel shared-string table.
func ooxmlContentPart(name string) bool {
	return name == "word/document.xml" ||
		name == "xl/sharedStrings.xml" ||
		strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") ||
		name == "docProps/core.xml"
}

// odfContentPart selects the OpenDocument text-bearing parts.
func odfContentPart(name string) bool {
	return name == "content.xml" || name == "meta.xml"
}

// parseZipXMLText opens the zip container, extracts character data from the
// selected XML parts, and builds the page. Sizes are bounded so a hostile
// container cannot balloon extraction.
func parseZipXMLText(
	rawURL string,
	body []byte,
	selectPart func(string) bool,
) (Document, bool) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Document{URL: rawURL}, false
	}
	var text strings.Builder
	parts := 0
	for _, file := range reader.File {
		if !selectPart(file.Name) || parts >= officeMaxParts {
			continue
		}
		parts++
		content, err := readZipPart(file)
		if err != nil {
			continue
		}
		appendXMLCharData(&text, content)
	}
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return Document{URL: rawURL}, false
	}

	return Document{
		URL:   rawURL,
		Title: textTitle(extracted),
		Text:  extracted,
	}, true
}

func readZipPart(file *zip.File) ([]byte, error) {
	opened, err := file.Open()
	if err != nil {
		return nil, err //nolint:wrapcheck // internal helper, caller skips on error.
	}
	defer func() { _ = opened.Close() }()

	//nolint:wrapcheck // internal helper, caller skips on error.
	return io.ReadAll(io.LimitReader(opened, officeMaxPartBytes))
}

// noiseMetaElement names the document-metadata elements whose text is machine
// bookkeeping (the authoring application, timestamps, edit counters) rather
// than content — indexing them pollutes the page with generator strings and
// ISO date stamps, and lets a generator string masquerade as the title.
var noiseMetaElement = set(
	"generator", "created", "modified", "date", "creation-date",
	"print-date", "editing-cycles", "editing-duration", "revision",
)

// appendXMLCharData writes the XML document's character data, breaking lines
// at the block elements OOXML and ODF use for paragraphs, rows, and pages and
// dropping the character data of the metadata-noise elements.
func appendXMLCharData(text *strings.Builder, content []byte) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	depth, skipDepth := 0, -1
	for {
		token, err := decoder.Token()
		if err != nil {
			return
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if skipDepth < 0 && noiseMetaElement[typed.Name.Local] {
				skipDepth = depth
			}
		case xml.CharData:
			appendXMLChars(text, typed, skipDepth)
		case xml.EndElement:
			if skipDepth == depth {
				skipDepth = -1
			}
			depth--
			switch typed.Name.Local {
			case "p", "row", "si", "h", "title":
				text.WriteByte('\n')
			}
		}
	}
}

// appendXMLChars writes trimmed character data unless it sits inside a
// metadata-noise element.
func appendXMLChars(text *strings.Builder, data xml.CharData, skipDepth int) {
	if skipDepth >= 0 {
		return
	}
	if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
		text.WriteString(trimmed)
		text.WriteByte(' ')
	}
}

// freeMindMap covers the FreeMind mind-map shape: nested nodes with TEXT
// attributes.
type freeMindMap struct {
	Root freeMindNode `xml:"node"`
}

type freeMindNode struct {
	Text     string         `xml:"TEXT,attr"`
	Children []freeMindNode `xml:"node"`
}

// parseFreeMind indexes a FreeMind .mm map as its node texts in tree order.
func parseFreeMind(rawURL string, body []byte) (Document, bool) {
	var parsed freeMindMap
	if xml.Unmarshal(body, &parsed) != nil {
		return Document{URL: rawURL}, false
	}
	var text strings.Builder
	appendFreeMindNode(&text, parsed.Root)
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return Document{URL: rawURL}, false
	}

	return Document{
		URL:   rawURL,
		Title: textTitle(extracted),
		Text:  extracted,
	}, true
}

func appendFreeMindNode(text *strings.Builder, node freeMindNode) {
	if trimmed := strings.TrimSpace(node.Text); trimmed != "" {
		text.WriteString(trimmed)
		text.WriteByte('\n')
	}
	for _, child := range node.Children {
		appendFreeMindNode(text, child)
	}
}
