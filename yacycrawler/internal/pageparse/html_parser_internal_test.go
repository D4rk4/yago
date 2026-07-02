package pageparse

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

func restoreParserSeams(
	t *testing.T,
	charsetReader func(io.Reader, string) (io.Reader, error),
	htmlParser func(io.Reader) (*html.Node, error),
	extractor func(io.Reader, trafilatura.Options) (*trafilatura.ExtractResult, error),
) {
	t.Helper()
	t.Cleanup(func() {
		newHTMLCharsetReader = charsetReader
		parseHTMLDocument = htmlParser
		extractReadableContent = extractor
	})
}

func TestParseHTMLFallsBackWhenCharsetReaderFails(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	newHTMLCharsetReader = func(io.Reader, string) (io.Reader, error) {
		return nil, errors.New("charset failed")
	}
	extractReadableContent = func(io.Reader, trafilatura.Options) (*trafilatura.ExtractResult, error) {
		return &trafilatura.ExtractResult{}, nil
	}

	page := ParseHTML(
		"http://example.com/",
		"text/html",
		[]byte("<html><body>fallback text</body></html>"),
	)

	if !strings.Contains(page.Text, "fallback text") {
		t.Fatalf("fallback text missing: %q", page.Text)
	}
}

func TestParseHTMLReturnsURLWhenHTMLParserFails(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	parseHTMLDocument = func(io.Reader) (*html.Node, error) {
		return nil, errors.New("parse failed")
	}

	page := ParseHTML("http://example.com/bad", "text/html", []byte("<html>"))

	if page.URL != "http://example.com/bad" {
		t.Fatalf("URL = %q", page.URL)
	}
	if page.Text != "" || len(page.Links) != 0 {
		t.Fatalf("parsed fields = %+v", page)
	}
}

func TestSelectTextFallsBackWhenExtractorFails(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	extractReadableContent = func(io.Reader, trafilatura.Options) (*trafilatura.ExtractResult, error) {
		return nil, errors.New("extract failed")
	}

	got := selectText("text/html", []byte("<html></html>"), " fallback \n text ")

	if got != "fallback text" {
		t.Fatalf("text = %q", got)
	}
}

func TestExtractMainContentReturnsExtractorError(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	sentinel := errors.New("extract failed")
	extractReadableContent = func(io.Reader, trafilatura.Options) (*trafilatura.ExtractResult, error) {
		return nil, sentinel
	}

	_, err := extractMainContent("text/html", []byte("<html></html>"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}
