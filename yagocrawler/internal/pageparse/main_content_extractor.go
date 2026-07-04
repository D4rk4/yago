package pageparse

import (
	"bytes"
	"fmt"

	"github.com/markusmobius/go-trafilatura"
)

var extractReadableContent = trafilatura.Extract

func extractMainContent(contentType string, body []byte) (string, error) {
	reader, err := newHTMLCharsetReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	result, err := extractReadableContent(reader, trafilatura.Options{
		ExcludeComments: true,
		EnableFallback:  true,
		Focus:           trafilatura.Balanced,
		HtmlDateMode:    trafilatura.Disabled,
	})
	if err != nil {
		return "", fmt.Errorf("trafilatura extract: %w", err)
	}
	return collapseSpaces(result.ContentText), nil
}
