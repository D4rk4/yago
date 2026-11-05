package pageparse

import (
	"io"
	"testing"

	"github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

func TestParseHTMLDecodesCharsetOnce(t *testing.T) {
	original := newHTMLCharsetReader
	calls := 0
	t.Cleanup(func() { newHTMLCharsetReader = original })
	newHTMLCharsetReader = func(reader io.Reader, contentType string) (io.Reader, error) {
		calls++
		return original(reader, contentType)
	}
	ParseHTML(
		"https://example.org/article",
		"text/html; charset=utf-8",
		[]byte(
			"<html><head><title>Harbor</title></head><body><p>Harbor engineering describes durable piers and safe berths.</p></body></html>",
		),
	)
	if calls != 1 {
		t.Fatalf("charset decodes = %d, want 1", calls)
	}
}

func TestParseHTMLPassesParsedRootToExtractor(t *testing.T) {
	original := newHTMLCharsetReader
	originalParser := parseHTMLDocument
	originalExtractor := extractReadableContent
	t.Cleanup(func() {
		newHTMLCharsetReader = original
		parseHTMLDocument = originalParser
		extractReadableContent = originalExtractor
	})

	var parsedRoot *html.Node
	parseHTMLDocument = func(reader io.Reader) (*html.Node, error) {
		node, err := originalParser(reader)
		if err != nil {
			return nil, err
		}
		parsedRoot = node
		return node, nil
	}

	var seenRoot *html.Node
	extractReadableContent = func(root *html.Node, opts trafilatura.Options) (*trafilatura.ExtractResult, error) {
		seenRoot = root
		return &trafilatura.ExtractResult{}, nil
	}

	ParseHTML(
		"https://example.org/article",
		"text/html; charset=utf-8",
		[]byte(
			"<html><head><title>Harbor</title></head><body><p>Harbor engineering describes durable piers and safe berths.</p></body></html>",
		),
	)

	if seenRoot != parsedRoot {
		t.Fatal("extractor did not receive the same *html.Node ParseHTML parsed")
	}
}
