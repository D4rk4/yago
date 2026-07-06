package formatparse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
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
// plain XML; the legacy binary formats (doc/xls/ppt/Visio — Apache POI
// territory) extract best-effort readable runs like MSG. Old StarOffice
// sxw/sxc use the ODF container too, so they route with it.
func parseOffice(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	ext := urlExtension(rawURL)
	switch {
	case ooxmlExtensions[ext]:
		return parseZipXMLText(rawURL, body, ooxmlContentPart)
	case odfExtensions[ext] || ext == "sxw" || ext == "sxc":
		return parseZipXMLText(rawURL, body, odfContentPart)
	case ext == "mm":
		return parseFreeMind(rawURL, body)
	case legacyOfficeExtensions[ext]:
		return parseMSG(rawURL, body)
	default:
		return pageparse.ParsedPage{URL: rawURL}, false
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
) (pageparse.ParsedPage, bool) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return pageparse.ParsedPage{URL: rawURL}, false
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
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
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

// appendXMLCharData writes the XML document's character data, breaking lines
// at the block elements OOXML and ODF use for paragraphs, rows, and pages.
func appendXMLCharData(text *strings.Builder, content []byte) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		token, err := decoder.Token()
		if err != nil {
			return
		}
		switch typed := token.(type) {
		case xml.CharData:
			trimmed := strings.TrimSpace(string(typed))
			if trimmed != "" {
				text.WriteString(trimmed)
				text.WriteByte(' ')
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "p", "row", "si", "h", "title":
				text.WriteByte('\n')
			}
		}
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
func parseFreeMind(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	var parsed freeMindMap
	if xml.Unmarshal(body, &parsed) != nil {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var text strings.Builder
	appendFreeMindNode(&text, parsed.Root)
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
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
