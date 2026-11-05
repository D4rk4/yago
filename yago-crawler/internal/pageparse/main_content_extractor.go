package pageparse

import (
	"fmt"

	"github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

var extractReadableContent = trafilatura.ExtractDocument

// extractMainContent pulls the page's main text with the FineWeb extraction
// recipe (arXiv:2406.17557): trafilatura favoring precision, duplicate
// segments removed, comments excluded — so navigation chrome and repeated
// boilerplate stay out of the RWI postings and snippets. EnableFallback keeps
// the readability/dom-distiller safety net as the recall floor, and a page
// where extraction still yields nothing falls back to the full-DOM text walk
// in selectText. The DOM is already decoded and parsed by ParseHTML, so the
// extractor reuses that single root instead of decoding and parsing again.
func extractMainContent(root *html.Node) (string, error) {
	result, err := extractReadableContent(root, trafilatura.Options{
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
