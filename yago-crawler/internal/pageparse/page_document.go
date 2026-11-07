package pageparse

import (
	"bytes"

	"golang.org/x/net/html"
)

func parsePageDocument(contentType string, body []byte) (*html.Node, error) {
	reader, err := newHTMLCharsetReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	return parseHTMLDocument(reader)
}
