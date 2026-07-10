package yagonode

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// Index export — YaCy's IndexExport_p parity (UI-18): stream the indexed
// documents as plain URL lines, CSV, or JSON-lines, optionally filtered by
// domain or URL substring. The export walks the stored corpus directly (not
// the sampled admin browser), so it is complete; it streams row by row and
// never buffers the corpus.

type indexExporter struct {
	docs documentstore.StoredDocuments
}

// newIndexExporter returns the console exporter, or nil without a corpus.
func newIndexExporter(docs documentstore.StoredDocuments) adminui.IndexExporter {
	if docs == nil {
		return nil
	}

	return indexExporter{docs: docs}
}

func (e indexExporter) ExportDocuments(
	ctx context.Context,
	req adminui.IndexExportRequest,
	w io.Writer,
) error {
	write, finish, err := exportRowWriter(req.Format, w)
	if err != nil {
		return err
	}
	err = e.docs.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("export cancelled: %w", err)
		}
		if !exportMatches(doc, req) {
			return true, nil
		}

		return true, write(doc)
	})
	if err != nil {
		return fmt.Errorf("walk corpus: %w", err)
	}

	return finish()
}

// exportMatches applies the operator's filters to one document.
func exportMatches(doc documentstore.Document, req adminui.IndexExportRequest) bool {
	if req.Domain != "" {
		host := exportDocumentHost(doc)
		if host != req.Domain && !strings.HasSuffix(host, "."+req.Domain) {
			return false
		}
	}
	if req.URLContains != "" &&
		!strings.Contains(strings.ToLower(doc.NormalizedURL), strings.ToLower(req.URLContains)) {
		return false
	}

	return true
}

// exportRowWriter picks the row encoder for one format.
func exportRowWriter(
	format string,
	w io.Writer,
) (write func(documentstore.Document) error, finish func() error, err error) {
	switch format {
	case "", "text":
		return func(doc documentstore.Document) error {
			if _, err := io.WriteString(w, doc.NormalizedURL+"\n"); err != nil {
				return fmt.Errorf("write url line: %w", err)
			}

			return nil
		}, func() error { return nil }, nil
	case "csv":
		return csvRowWriter(w)
	case "jsonl":
		encoder := json.NewEncoder(w)

		return func(doc documentstore.Document) error {
			if err := encoder.Encode(exportJSONRow(doc)); err != nil {
				return fmt.Errorf("write json line: %w", err)
			}

			return nil
		}, func() error { return nil }, nil
	default:
		return nil, nil, fmt.Errorf("unknown export format %q", format)
	}
}

func exportCSVRow(doc documentstore.Document) []string {
	return []string{
		doc.NormalizedURL, doc.Title, doc.ContentType, doc.Language,
		doc.IndexedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

type exportJSONDocument struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Language    string `json:"language,omitempty"`
	IndexedAt   string `json:"indexedAt,omitempty"`
}

func exportJSONRow(doc documentstore.Document) exportJSONDocument {
	row := exportJSONDocument{
		URL: doc.NormalizedURL, Title: doc.Title,
		ContentType: doc.ContentType, Language: doc.Language,
	}
	if !doc.IndexedAt.IsZero() {
		row.IndexedAt = doc.IndexedAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	return row
}

// exportDocumentHost extracts the lowercase host of a stored document.
func exportDocumentHost(doc documentstore.Document) string {
	parsed, err := url.Parse(doc.NormalizedURL)
	if err != nil {
		return ""
	}

	return strings.ToLower(parsed.Hostname())
}

// csvRowWriter emits the header eagerly and rows as they stream.
func csvRowWriter(
	w io.Writer,
) (write func(documentstore.Document) error, finish func() error, err error) {
	encoder := csv.NewWriter(w)
	header := []string{"url", "title", "content_type", "language", "indexed_at"}
	// Flush the header eagerly so a broken sink surfaces at construction; csv
	// buffers writes, so a bare Write never reaches the sink and Error() reports
	// the failure only after a Flush.
	_ = encoder.Write(header)
	encoder.Flush()
	if err := encoder.Error(); err != nil {
		return nil, nil, fmt.Errorf("write csv header: %w", err)
	}
	write = func(doc documentstore.Document) error {
		if err := encoder.Write(exportCSVRow(doc)); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}

		return nil
	}
	finish = func() error {
		encoder.Flush()
		if err := encoder.Error(); err != nil {
			return fmt.Errorf("flush csv: %w", err)
		}

		return nil
	}

	return write, finish, nil
}
