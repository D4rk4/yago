package pageparse

import (
	"bytes"

	"golang.org/x/net/html"
)

// decodeAndParseHTML decodes the body per its declared content type and parses
// it into a single HTML document, so both the field parser and the main-content
// extractor share one decode and one parse. A content type whose charset cannot
// be decoded falls back to parsing the raw bytes.
func decodeAndParseHTML(contentType string, body []byte) (*html.Node, error) {
	reader, err := newHTMLCharsetReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	return parseHTMLDocument(reader)
}
