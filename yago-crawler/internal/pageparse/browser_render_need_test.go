package pageparse

import (
	"errors"
	"io"
	"testing"

	"github.com/markusmobius/go-trafilatura"
)

func TestBrowserRenderNeededUsesStructuralContentEvidence(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        bool
	}{
		{
			name:        "external script shell",
			contentType: "text/html",
			body:        `<html><head><title>Portal</title></head><body><div></div><script src="/app.js"></script></body></html>`,
			want:        true,
		},
		{
			name:        "inline script shell",
			contentType: "text/html; broken",
			body:        `<html><body><main>Wait</main><script>document.body.dataset.ready = "1"</script></body></html>`,
			want:        true,
		},
		{
			name:        "module shell",
			contentType: "application/xhtml+xml",
			body:        `<html><body><div></div><script type="module" src="/app.js" /></body></html>`,
			want:        true,
		},
		{
			name:        "usable static prose",
			contentType: "text/html",
			body:        `<html><body><article>Static content already contains enough searchable evidence for this page.</article><script src="/metrics.js"></script></body></html>`,
		},
		{
			name:        "usable unsegmented text",
			contentType: "text/html",
			body:        `<html><body><main>这是无需浏览器即可索引的正文内容</main><script src="/metrics.js"></script></body></html>`,
		},
		{
			name:        "scriptless empty page",
			contentType: "text/html",
			body:        `<html><body><div></div></body></html>`,
		},
		{
			name:        "structured data only",
			contentType: "text/html",
			body:        `<html><body><div></div><script type="application/ld+json">{"name":"page"}</script></body></html>`,
		},
		{
			name:        "empty executable script",
			contentType: "text/html",
			body:        `<html><body><div></div><script type="text/javascript"></script></body></html>`,
		},
		{
			name:        "non HTML",
			contentType: "application/pdf",
			body:        `<script src="/app.js"></script>`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := BrowserRenderNeeded(test.contentType, []byte(test.body)); got != test.want {
				t.Fatalf("BrowserRenderNeeded() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestBrowserRenderNeededTreatsExtractionFailureAsMissingContent(t *testing.T) {
	restoreParserSeams(t, newHTMLCharsetReader, parseHTMLDocument, extractReadableContent)
	extractReadableContent = func(io.Reader, trafilatura.Options) (*trafilatura.ExtractResult, error) {
		return nil, errors.New("extract failed")
	}
	if !BrowserRenderNeeded(
		"text/html",
		[]byte(`<html><body><script src="/app.js"></script></body></html>`),
	) {
		t.Fatal("extraction failure did not request browser rendering")
	}
}

func TestStaticContentUsableCountsTermsAndRunes(t *testing.T) {
	if !staticContentUsable("one two three four") {
		t.Fatal("term threshold rejected usable content")
	}
	if !staticContentUsable("abcdefghijklmnop") {
		t.Fatal("rune threshold rejected usable content")
	}
	if staticContentUsable("short") {
		t.Fatal("insufficient text accepted as usable content")
	}
}

func TestExecutableScriptTypeUsesScriptSyntax(t *testing.T) {
	for _, value := range []string{"", "module", "text/javascript; charset=utf-8", "application/ecmascript"} {
		if !executableScriptType(value) {
			t.Fatalf("script type %q rejected", value)
		}
	}
	for _, value := range []string{"application/ld+json", "importmap", "text/plain"} {
		if executableScriptType(value) {
			t.Fatalf("non-executable script type %q accepted", value)
		}
	}
}
