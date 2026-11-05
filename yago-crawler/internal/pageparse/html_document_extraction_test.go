package pageparse

import (
	"errors"
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
	originalParser := parseHTMLDocument
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)

	var parsedRoot *html.Node
	parseHTMLDocument = func(reader io.Reader) (*html.Node, error) {
		node, err := originalParser(reader)
		if err != nil {
			return nil, err
		}
		parsedRoot = node
		return node, nil
	}

	called := false
	var seenRoot *html.Node
	extractReadableContent = func(root *html.Node, opts trafilatura.Options) (*trafilatura.ExtractResult, error) {
		called = true
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

	if !called {
		t.Fatal("extractor was not called")
	}
	if parsedRoot == nil {
		t.Fatal("ParseHTML did not parse a root")
	}
	if seenRoot != parsedRoot {
		t.Fatal("extractor did not receive the same *html.Node ParseHTML parsed")
	}
}

func TestBrowserRenderNeededReturnsTrueWhenParseFails(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	parseHTMLDocument = func(io.Reader) (*html.Node, error) {
		return nil, errors.New("parse failed")
	}

	if !BrowserRenderNeeded(
		"text/html",
		[]byte(`<html><body><script src="/app.js"></script></body></html>`),
	) {
		t.Fatal("parse failure did not request browser rendering")
	}
}

func TestDecodeAndParseHTMLFallsBackToRawBytesOnBadCharset(t *testing.T) {
	original := newHTMLCharsetReader
	t.Cleanup(func() { newHTMLCharsetReader = original })
	newHTMLCharsetReader = func(io.Reader, string) (io.Reader, error) {
		return nil, errors.New("charset failed")
	}

	root, err := decodeAndParseHTML("text/html", []byte("<html><body>raw</body></html>"))
	if err != nil {
		t.Fatalf("decodeAndParseHTML: %v", err)
	}
	if root == nil {
		t.Fatal("decodeAndParseHTML returned nil root")
	}
}
