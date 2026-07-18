package formatparse

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestMarkdownAdmissionByExtensionAndMIME(t *testing.T) {
	enabled := yagocrawlcontract.FormatToggles{Text: true}
	disabled := yagocrawlcontract.FormatToggles{}
	tests := []struct {
		name        string
		url         string
		contentType string
	}{
		{
			name:        "short extension",
			url:         "https://example.test/README.md",
			contentType: "application/octet-stream",
		},
		{
			name:        "long extension",
			url:         "https://example.test/guide.markdown",
			contentType: "application/octet-stream",
		},
		{
			name:        "standard MIME",
			url:         "https://example.test/download",
			contentType: "text/markdown; charset=utf-8",
		},
		{name: "legacy MIME", url: "https://example.test/download", contentType: "text/x-markdown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !Accepts(test.url, test.contentType, enabled) {
				t.Fatal("Markdown document rejected with text formats enabled")
			}
			if Accepts(test.url, test.contentType, disabled) {
				t.Fatal("Markdown document admitted with text formats disabled")
			}
		})
	}
}

func TestMarkdownOctetStreamParsing(t *testing.T) {
	toggles := yagocrawlcontract.FormatToggles{Text: true}
	body := []byte("# Search architecture\n\nLocal and federated retrieval.")
	for _, extension := range []string{"md", "markdown"} {
		page, parsed := Parse(
			"https://example.test/README."+extension,
			"application/octet-stream",
			body,
			toggles,
		)
		if !parsed || page.Title != "# Search architecture" ||
			!strings.Contains(page.Text, "federated retrieval") {
			t.Fatalf("Markdown parse for .%s = %t %+v", extension, parsed, page)
		}
	}
}

func TestMarkdownMIMEParsing(t *testing.T) {
	body := []byte("# MIME-routed document\n\nIndexed without a URL extension.")
	for _, contentType := range []string{"text/markdown; charset=utf-8", "text/x-markdown"} {
		page, parsed := Parse(
			"https://example.test/download",
			contentType,
			body,
			yagocrawlcontract.FormatToggles{Text: true},
		)
		if !parsed || page.Title != "# MIME-routed document" {
			t.Fatalf("Markdown MIME parse for %q = %t %+v", contentType, parsed, page)
		}
		if _, parsed := Parse(
			"https://example.test/download",
			contentType,
			body,
			yagocrawlcontract.FormatToggles{},
		); parsed {
			t.Fatalf("Markdown MIME %q parsed with text formats disabled", contentType)
		}
	}
}

func TestMarkdownBinaryBodyFailsClosed(t *testing.T) {
	body := make([]byte, 4096)
	copy(body[128:], []byte("binary payload"))
	if _, parsed := Parse(
		"https://example.test/README.md",
		"application/octet-stream",
		body,
		yagocrawlcontract.FormatToggles{Text: true},
	); parsed {
		t.Fatal("binary Markdown body parsed")
	}
}

func TestMarkdownURLFetchAdmissionUsesTextToggle(t *testing.T) {
	if !URLFetchAllowed(
		"https://example.test/README.md",
		yagocrawlcontract.FormatToggles{Text: true},
	) {
		t.Fatal("Markdown URL rejected with text formats enabled")
	}
	if URLFetchAllowed(
		"https://example.test/README.markdown",
		yagocrawlcontract.FormatToggles{},
	) {
		t.Fatal("Markdown URL admitted with text formats disabled")
	}
}
