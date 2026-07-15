package yagonode

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	documentBrowseLimit = 50
	// documentScanBudget bounds how many stored documents a filtered browse
	// may decode: every visited document is a vault read plus a zstd+JSON
	// decode of the full text, so an unbounded scan made /admin/index take
	// tens of seconds on a large store (PERF-01).
	documentScanBudget = 20000
)

// documentBrowseSource lists indexed documents for the admin Index browser by
// scanning the document store and filtering by URL substring and/or domain.
// The scan is bounded: an unfiltered browse stops as soon as the page is
// full, and a filtered browse gives up after documentScanBudget documents,
// marking the page as sampled instead of scanning the whole store.
type documentBrowseSource struct {
	stored documentstore.StoredDocuments
}

func newDocumentBrowseSource(stored documentstore.StoredDocuments) documentBrowseSource {
	return documentBrowseSource{stored: stored}
}

func (s documentBrowseSource) BrowseDocuments(
	ctx context.Context,
	query adminui.DocumentQuery,
) adminui.DocumentPage {
	needle := strings.ToLower(strings.TrimSpace(query.URLContains))
	domain := strings.ToLower(strings.TrimSpace(query.Domain))
	filtered := needle != "" || domain != ""

	var matches []documentstore.Document
	matched := 0
	scanned := 0
	sampled := false
	scanFailed := false
	if err := s.stored.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		scanned++
		if filtered && scanned > documentScanBudget {
			sampled = true

			return false, nil
		}
		if !documentMatches(doc, needle, domain) {
			return true, nil
		}
		matched++
		if len(matches) < documentBrowseLimit {
			matches = append(matches, doc)
		}
		if !filtered && len(matches) >= documentBrowseLimit {
			sampled = true

			return false, nil
		}

		return true, nil
	}); err != nil {
		scanFailed = true
		slog.WarnContext(ctx, "browse documents scan failed", slog.Any("error", err))
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].IndexedAt.After(matches[j].IndexedAt)
	})

	summaries := make([]adminui.DocumentSummary, 0, len(matches))
	for _, doc := range matches {
		summaries = append(summaries, documentSummary(doc))
	}

	return adminui.DocumentPage{
		Documents:  summaries,
		Matched:    matched,
		Limit:      documentBrowseLimit,
		Truncated:  matched > len(summaries),
		ScanFailed: scanFailed,
		Sampled:    sampled,
	}
}

func documentMatches(doc documentstore.Document, needle, domain string) bool {
	link := documentURL(doc)
	if needle != "" && !strings.Contains(strings.ToLower(link), needle) {
		return false
	}
	if domain != "" && !documentDomainMatches(link, domain) {
		return false
	}

	return true
}

func documentDomainMatches(rawURL, domain string) bool {
	host := documentHost(rawURL)

	return host == domain || strings.HasSuffix(host, "."+domain)
}

func documentHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return strings.ToLower(parsed.Hostname())
}

func documentURL(doc documentstore.Document) string {
	if doc.CanonicalURL != "" {
		return doc.CanonicalURL
	}

	return doc.NormalizedURL
}

func documentSummary(doc documentstore.Document) adminui.DocumentSummary {
	return adminui.DocumentSummary{
		URL:         documentURL(doc),
		Key:         doc.NormalizedURL,
		Title:       doc.Title,
		ContentType: doc.ContentType,
		Language:    doc.Language,
		FetchedAt:   formatDocumentTime(doc.FetchedAt),
		IndexedAt:   formatDocumentTime(doc.IndexedAt),
	}
}

func formatDocumentTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}
