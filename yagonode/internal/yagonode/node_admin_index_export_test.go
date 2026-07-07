package yagonode

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type exportCorpus []documentstore.Document

func (c exportCorpus) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range c {
		if ok, err := visit(doc); !ok || err != nil {
			return err
		}
	}

	return nil
}

func exportFixture() exportCorpus {
	return exportCorpus{
		{
			NormalizedURL: "https://a.example/page", Title: "A, page", ContentType: "text/html",
			Language: "en", IndexedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		},
		{NormalizedURL: "https://sub.a.example/two", Title: "Sub"},
		{NormalizedURL: "https://b.example/other", Title: "B"},
	}
}

// TestIndexExportFormatsAndFilters pins UI-18: text streams URL lines, the
// domain filter keeps subdomains, the substring filter narrows further, CSV
// quotes fields and carries the header, JSONL emits one object per row.
func TestIndexExportFormatsAndFilters(t *testing.T) {
	exporter := newIndexExporter(exportFixture())
	ctx := context.Background()

	var text strings.Builder
	if err := exporter.ExportDocuments(ctx, adminui.IndexExportRequest{
		Format: "text", Domain: "a.example",
	}, &text); err != nil {
		t.Fatalf("text export: %v", err)
	}
	if text.String() != "https://a.example/page\nhttps://sub.a.example/two\n" {
		t.Fatalf("text = %q", text.String())
	}

	var narrowed strings.Builder
	if err := exporter.ExportDocuments(ctx, adminui.IndexExportRequest{
		Domain: "a.example", URLContains: "TWO",
	}, &narrowed); err != nil {
		t.Fatalf("narrowed export: %v", err)
	}
	if narrowed.String() != "https://sub.a.example/two\n" {
		t.Fatalf("narrowed = %q", narrowed.String())
	}

	var asCSV strings.Builder
	if err := exporter.ExportDocuments(ctx, adminui.IndexExportRequest{
		Format: "csv", Domain: "a.example", URLContains: "page",
	}, &asCSV); err != nil {
		t.Fatalf("csv export: %v", err)
	}
	if !strings.HasPrefix(asCSV.String(), "url,title,content_type,language,indexed_at\n") ||
		!strings.Contains(asCSV.String(), `"A, page"`) ||
		!strings.Contains(asCSV.String(), "2026-07-01T12:00:00Z") {
		t.Fatalf("csv = %q", asCSV.String())
	}

	var asJSON strings.Builder
	if err := exporter.ExportDocuments(ctx, adminui.IndexExportRequest{
		Format: "jsonl", Domain: "b.example",
	}, &asJSON); err != nil {
		t.Fatalf("jsonl export: %v", err)
	}
	if !strings.Contains(asJSON.String(), `"url":"https://b.example/other"`) ||
		strings.Contains(asJSON.String(), "indexedAt") {
		t.Fatalf("jsonl = %q", asJSON.String())
	}

	if err := exporter.ExportDocuments(ctx, adminui.IndexExportRequest{
		Format: "xml",
	}, &strings.Builder{}); err == nil {
		t.Fatal("unknown format must fail")
	}
	if newIndexExporter(nil) != nil {
		t.Fatal("nil corpus must disable the exporter")
	}
}
