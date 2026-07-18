package pageparse

import (
	"bytes"
	"fmt"

	"github.com/markusmobius/go-trafilatura"
)

var extractReadableContent = trafilatura.Extract

// extractMainContent pulls the page's main text with the FineWeb extraction
// recipe (arXiv:2406.17557): trafilatura favoring precision, duplicate
// segments removed, comments excluded — so navigation chrome and repeated
// boilerplate stay out of the RWI postings and snippets. EnableFallback keeps
// the readability/dom-distiller safety net as the recall floor, and a page
// where extraction still yields nothing falls back to the full-DOM text walk
// in selectText.
func extractMainContent(contentType string, body []byte) (string, error) {
	reader, err := newHTMLCharsetReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	result, err := extractReadableContent(reader, trafilatura.Options{
		ExcludeComments: true,
		EnableFallback:  true,
		Focus:           trafilatura.FavorPrecision,
		Deduplicate:     true,
		HtmlDateMode:    trafilatura.Disabled,
	})
	if err != nil {
		return "", fmt.Errorf("trafilatura extract: %w", err)
	}
	return collapseSpaces(result.ContentText), nil
}
