package formatparse

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// familySample is one end-to-end probe: the body a server would send, the
// declared content type, and the marker the parser must extract.
type familySample struct {
	url        string
	mime       string
	body       func(t *testing.T) []byte
	wantText   string
	wantTitle  string
	skipReason string
}

// formatSamples covers every parser family with a representative document.
func formatSamples() map[string]familySample {
	samples := textualSamples()
	for name, sample := range binarySamples() {
		samples[name] = sample
	}

	return samples
}

// textualSamples are the text-carrying families.
func textualSamples() map[string]familySample {
	return map[string]familySample{
		"html": {
			url: "https://a.example/page.html", mime: "text/html",
			body: func(*testing.T) []byte {
				return []byte("<html><head><title>Page Title</title></head>" +
					"<body><p>Body text here.</p></body></html>")
			},
			wantText: "Body text here.", wantTitle: "Page Title",
		},
		"text": {
			url: "https://a.example/notes.txt", mime: "text/plain",
			body: func(*testing.T) []byte {
				return []byte("Plain note about crawler formats.\nSecond line.")
			},
			wantText: "Plain note about crawler formats.",
		},
		"csv": {
			url: "https://a.example/table.csv", mime: "text/csv",
			body: func(*testing.T) []byte {
				return []byte("name,city\nErika,Bremen\nJonas,Kiel\n")
			},
			wantText: "Erika",
		},
		"xmlfeeds": {
			url: "https://a.example/feed.rss", mime: "application/rss+xml",
			body: func(*testing.T) []byte {
				return []byte(`<?xml version="1.0"?><rss version="2.0"><channel>` +
					`<title>Feed Title</title><item><title>Entry One</title>` +
					`<description>Entry body words</description>` +
					`<link>https://a.example/one</link></item></channel></rss>`)
			},
			wantText: "Entry One",
		},
	}
}

// binarySamples are the families whose payloads need binary builders.
func binarySamples() map[string]familySample {
	return map[string]familySample{
		"pdf": {
			url: "https://a.example/doc.pdf", mime: "application/pdf",
			body: func(t *testing.T) []byte {
				return buildLegacyPDF(t, "BT (Legacy pdf body sentence) Tj ET")
			},
			wantText: "Legacy pdf body sentence",
		},
		"office": {
			url:  "https://a.example/report.docx",
			mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			body: func(t *testing.T) []byte {
				return zipBody(t, map[string]string{
					"word/document.xml": `<?xml version="1.0"?>` +
						`<w:document xmlns:w="ns"><w:body><w:p><w:r>` +
						`<w:t>Quarterly report paragraph.</w:t>` +
						`</w:r></w:p></w:body></w:document>`,
				})
			},
			wantText: "Quarterly report paragraph.",
		},
		"images": {
			url: "https://a.example/photos/pic.png", mime: "image/png",
			body:     pngImage,
			wantText: "PNG 12x7", wantTitle: "pic.png",
		},
		"audio": {
			url: "https://a.example/track.mp3", mime: "audio/mpeg",
			body:     mp3WithTags,
			wantText: "Artist: The Waves", wantTitle: "Night Drive",
		},
		"misc": {
			url: "https://a.example/contact.vcf", mime: "text/vcard",
			body: func(*testing.T) []byte {
				return []byte("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Erika Mustermann\r\n" +
					"ORG:Example Corp\r\nEND:VCARD\r\n")
			},
			wantText: "ORG: Example Corp", wantTitle: "Erika Mustermann",
		},
		"archives": {
			url: "https://a.example/bundle.zip", mime: "application/zip",
			body: func(t *testing.T) []byte {
				var buf bytes.Buffer
				writer := zip.NewWriter(&buf)
				entry, err := writer.Create("readme.txt")
				if err != nil {
					t.Fatalf("zip create: %v", err)
				}
				if _, err := entry.Write([]byte("Archived readme sentence.")); err != nil {
					t.Fatalf("zip write: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("zip close: %v", err)
				}

				return buf.Bytes()
			},
			wantText: "Archived readme sentence.",
		},
	}
}

// TestEveryFormatPassesGateAndExtracts is the operator's end-to-end demand
// (CRAWL-17 wrap-up): for EVERY family the fetch gate admits the document
// under all-on toggles, the parser produces an indexable page, and the
// extraction carries the expected text (and title where the format has one).
// The same sample must be REJECTED at the gate once its family is toggled
// off, proving the gate and the parser consult the same registry.
func TestEveryFormatPassesGateAndExtracts(t *testing.T) {
	allOnToggles := yagocrawlcontract.FormatToggles{
		Text: true, XMLFeeds: true, PDF: true, Office: true,
		Images: true, Audio: true, Misc: true, Archives: true,
	}
	for name, sample := range formatSamples() {
		if sample.skipReason != "" {
			t.Logf("%s: skipped: %s", name, sample.skipReason)

			continue
		}
		body := sample.body(t)
		if !Accepts(sample.url, sample.mime, allOnToggles) {
			t.Fatalf("%s: gate rejected %q despite the toggle on", name, sample.mime)
		}
		page, parsed := Parse(sample.url, sample.mime, body, allOnToggles)
		if !parsed {
			t.Fatalf("%s: gate admitted but parser produced nothing", name)
		}
		if sample.wantText != "" && !strings.Contains(page.Text, sample.wantText) {
			t.Fatalf("%s: extraction misses %q in %q", name, sample.wantText, page.Text)
		}
		if sample.wantTitle != "" && page.Title != sample.wantTitle {
			t.Fatalf("%s: title = %q, want %q", name, page.Title, sample.wantTitle)
		}
		if name == "html" {
			continue
		}
		if Accepts(sample.url, sample.mime, yagocrawlcontract.FormatToggles{}) {
			t.Fatalf("%s: gate must reject the family with its toggle off", name)
		}
	}
}
