package formatparse

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestParseRejectsMislabeledUnknownBinary(t *testing.T) {
	body := make([]byte, 4096)
	copy(body[128:], []byte("CD001 binary disk image"))
	page, parsed := Parse(
		"https://example.test/download",
		"text/plain; charset=utf-8",
		body,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if parsed || page.URL != "https://example.test/download" {
		t.Fatalf("mislabeled binary parse = %t %+v", parsed, page)
	}
	if _, parsed := Parse(
		"https://example.test/download",
		"text/html",
		body,
		yagocrawlcontract.DefaultFormatToggles(),
	); parsed {
		t.Fatal("mislabeled binary HTML parsed")
	}
}

func TestParseRejectsBinaryPlainTextExtension(t *testing.T) {
	body := make([]byte, 1024)
	copy(body[64:], []byte("binary payload"))
	for _, contentType := range []string{"text/plain", "application/octet-stream"} {
		if _, parsed := Parse(
			"https://example.test/payload.txt",
			contentType,
			body,
			yagocrawlcontract.DefaultFormatToggles(),
		); parsed {
			t.Fatalf("binary text parsed for %q", contentType)
		}
	}
}

func TestParseKeepsUnicodeTextAndHTMLFallbacks(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	plain, parsed := Parse(
		"https://example.test/download",
		"text/plain",
		[]byte("שלום עולם — مرحبا بالعالم"),
		toggles,
	)
	if !parsed || !strings.Contains(plain.Text, "שלום") {
		t.Fatalf("unicode text parse = %t %+v", parsed, plain)
	}
	html, parsed := Parse(
		"https://example.test/download",
		"text/html",
		[]byte(
			"\xef\xbb\xbf \n\t<html><head><title>Real page</title></head><body>text</body></html>",
		),
		toggles,
	)
	if !parsed || html.Title != "Real page" {
		t.Fatalf("HTML parse = %t %+v", parsed, html)
	}
}

func TestParseKnownExtensionPrecedesBinarySniffFallback(t *testing.T) {
	body := pdfWithText(t, "Known document", "BT (Searchable document) Tj ET")
	page, parsed := Parse(
		"https://example.test/document.pdf",
		"text/plain",
		body,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Known document" {
		t.Fatalf("known extension parse = %t %+v", parsed, page)
	}
}
