package crawlresults

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// TestQualityGateSkipsDocumentFormats pins the CRAWL-17 wrap-up: the spam
// heuristics judge web pages only — a short PDF extraction passes while the
// same short text on an HTML page is still rejected.
func TestQualityGateSkipsDocumentFormats(t *testing.T) {
	shortText := "Navy Public Service Award goes to Michoud executive"
	consumer := &IngestConsumer{quality: func(string) string { return "too short" }}

	pdfBatch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL: "http://a.example/doc.pdf",
		ContentType:   "application/pdf",
		ExtractedText: shortText,
	}}
	if rule := consumer.qualityRejectionRule(pdfBatch); rule != "" {
		t.Fatalf("pdf must skip the web-page gate, got %q", rule)
	}

	htmlBatch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL: "http://a.example/page.html",
		ContentType:   "text/html; charset=utf-8",
		ExtractedText: shortText,
	}}
	if rule := consumer.qualityRejectionRule(htmlBatch); rule != "too short" {
		t.Fatalf("html must face the gate, got %q", rule)
	}

	for _, webType := range []string{"", "text/plain", "application/xhtml+xml"} {
		batch := htmlBatch
		batch.Document.ContentType = webType
		if rule := consumer.qualityRejectionRule(batch); rule != "too short" {
			t.Fatalf("%q must face the gate, got %q", webType, rule)
		}
	}
}
