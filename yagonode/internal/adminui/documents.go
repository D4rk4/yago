package adminui

import (
	"context"
	"io"
)

// DocumentSummary is one indexed document as shown in the Index document browser.
// Key is the store key (the normalized URL) used to delete the document.
type DocumentSummary struct {
	URL         string
	Key         string
	Title       string
	ContentType string
	Language    string
	FetchedAt   string
	IndexedAt   string
}

// DocumentQuery narrows the document browser by a URL substring and/or a domain
// (an exact host or any of its subdomains). Empty fields match everything.
type DocumentQuery struct {
	URLContains string
	Domain      string
}

// DocumentPage is a bounded slice of matching documents plus how many matched in
// total, so the browser can note when results were truncated.
type DocumentPage struct {
	Documents  []DocumentSummary
	Matched    int
	Limit      int
	Truncated  bool
	ScanFailed bool
	// Sampled marks a page assembled without a full store scan: the browse
	// stopped early (unfiltered page) or hit its scan budget (filtered), so
	// Matched counts only what was seen, not the whole store.
	Sampled bool
}

// DocumentBrowserSource lists indexed documents matching a query, newest results
// first, bounded to a fixed page size. A nil provider hides the document browser.
type DocumentBrowserSource interface {
	BrowseDocuments(ctx context.Context, query DocumentQuery) DocumentPage
}

// IndexAdminSource performs destructive index maintenance: removing one document
// by its store key, or every document of a domain. It removes the document from
// every index lineage it participates in. A nil provider hides the delete
// controls.
type IndexAdminSource interface {
	DeleteDocument(ctx context.Context, key string) error
	DeleteDomain(ctx context.Context, domain string) (int, error)
}

// BlacklistEntry is one denylisted URL or domain as shown in the blacklist
// manager. Kind is "url" or "domain"; AddedAt is a preformatted timestamp.
type BlacklistEntry struct {
	Kind    string
	Value   string
	AddedAt string
}

// BlacklistSource manages the operator URL/domain denylist: listing entries and
// adding or removing them. Denylisted entries are filtered out of search results.
// A nil provider hides the blacklist manager.
type BlacklistSource interface {
	BlacklistEntries(ctx context.Context) ([]BlacklistEntry, error)
	AddBlacklist(ctx context.Context, kind, value string) error
	RemoveBlacklist(ctx context.Context, kind, value string) error
}

// BlacklistProber answers whether one URL would be blocked right now —
// YaCy's BlacklistTest_p parity (UI-17). Implemented by the same source.
type BlacklistProber interface {
	BlacklistBlocks(ctx context.Context, rawURL string) (bool, error)
}

// BlacklistPorter imports and exports the denylist as plaintext lines
// ("kind value" per line) — YaCy's BlacklistImpExp_p parity (UI-17).
type BlacklistPorter interface {
	ExportBlacklist(ctx context.Context) (string, error)
	ImportBlacklist(ctx context.Context, payload string) (added int, err error)
}

// IndexExportRequest filters one index export (UI-18, YaCy IndexExport_p).
type IndexExportRequest struct {
	// Format is text (URL per line), csv, or jsonl.
	Format string
	// Domain keeps documents of one host and its subdomains.
	Domain string
	// URLContains keeps documents whose URL contains the substring.
	URLContains string
}

// IndexExporter streams matching documents to the response; the console sets
// the content type and attachment headers per format. A nil provider hides
// the export controls.
type IndexExporter interface {
	ExportDocuments(ctx context.Context, req IndexExportRequest, w io.Writer) error
}
