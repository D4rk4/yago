package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeTerms struct {
	report TermReport
	last   string
}

func (f *fakeTerms) LookupTerm(_ context.Context, term string) TermReport {
	f.last = term

	return f.report
}

func indexSchemaFixture() []SchemaGroup {
	return []SchemaGroup{{
		Title:  "Full-text search index",
		Fields: []SchemaField{{Name: "body", Description: "Main extracted body text."}},
	}}
}

func TestConsoleIndexRendersTermBrowserAndSchema(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{
			Available:  true,
			Documents:  3,
			DiskSize:   "2.0 KiB",
			VaultUsed:  "1.5 MiB",
			VaultQuota: "unlimited",
		}},
		Terms:  &fakeTerms{},
		Schema: indexSchemaFixture(),
	})
	got := do(t, console, "/admin/index")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Term browser", `name="term"`, "Index schema",
		"Full-text search index", "body", "Main extracted body text.",
		"Search index on disk", "2.0 KiB", "Data vault on disk", "1.5 MiB",
		"Vault quota (maximum)", "unlimited",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("index page missing %q", want)
		}
	}
}

func TestConsoleIndexLabelsUnavailableStorageFacts(t *testing.T) {
	t.Parallel()

	body := do(t, New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
	}), "/admin/index").body
	for _, want := range []string{
		`<tr><th scope="row">Backend</th><td>Unavailable</td></tr>`,
		`<tr><th scope="row">Last updated</th><td>Not recorded</td></tr>`,
		`<tr><th scope="row">Search index on disk</th><td>Unavailable</td></tr>`,
		`<tr><th scope="row">Data vault on disk</th><td>Unavailable</td></tr>`,
		`<tr><th scope="row">Vault quota (maximum)</th><td>Unavailable</td></tr>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index status missing %q", want)
		}
	}
}

func TestConsoleIndexTermLookupShowsResults(t *testing.T) {
	t.Parallel()

	terms := &fakeTerms{report: TermReport{
		Term:  "golang",
		Hash:  "WWWWWWWWWWWW",
		Count: 2,
		Sample: []TermPosting{
			{URL: "http://a.example/1", Title: "Alpha"},
		},
	}}
	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: terms,
	})

	got := do(t, console, "/admin/index?term=golang")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if terms.last != "golang" {
		t.Fatalf("term not passed to source: %q", terms.last)
	}
	for _, want := range []string{
		"2 posting(s)", "Resolved 1 document(s) in this bounded sample.",
		"WWWWWWWWWWWW", "http://a.example/1", "Alpha",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("term results missing %q", want)
		}
	}
}

func TestConsoleIndexTermSampleUnavailableKeepsKnownCount(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: &fakeTerms{report: TermReport{
			Term:        "golang",
			Hash:        "WWWWWWWWWWWW",
			Count:       5,
			SampleError: errors.New("scan failed"),
		}},
	})
	body := do(t, console, "/admin/index?term=golang").body
	for _, want := range []string{
		"5 posting(s)",
		"The document sample is unavailable. The posting count above is still available.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unavailable sample missing %q", want)
		}
	}
	if strings.Contains(body, "Showing up to 0") {
		t.Fatal("failed sample rendered a zero-sized successful sample")
	}
}

func TestConsoleIndexTermSampleFailureLabelsResolvedRowsPartial(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: &fakeTerms{report: TermReport{
			Term:        "golang",
			Hash:        "WWWWWWWWWWWW",
			Count:       5,
			Sample:      []TermPosting{{URL: "https://partial.example/"}},
			SampleError: errors.New("rows failed"),
		}},
	})
	body := do(t, console, "/admin/index?term=golang").body
	for _, want := range []string{
		"5 posting(s)",
		"Resolved 1 document(s) before sampling failed; this is a partial sample.",
		"https://partial.example/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial sample missing %q", want)
		}
	}
}

func TestConsoleIndexTermLookupUnavailableIsGeneric(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: &fakeTerms{report: TermReport{
			Term:  "golang",
			Error: errors.New("private storage detail"),
		}},
	})
	body := do(t, console, "/admin/index?term=golang").body
	if !strings.Contains(body, "The term lookup is unavailable.") {
		t.Fatal("term lookup failure should render unavailable")
	}
	if strings.Contains(body, "private storage detail") {
		t.Fatal("term lookup leaked a storage error")
	}
}

func TestConsoleIndexTermNotFound(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: &fakeTerms{report: TermReport{Term: "absent", Hash: "H", NotFound: true}},
	})
	got := do(t, console, "/admin/index?term=absent")
	if !strings.Contains(got.body, "No postings for") {
		t.Fatal("expected the not-found message")
	}
}

func TestConsoleIndexOmitsTermBrowserWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}}), "/admin/index")
	if strings.Contains(got.body, "Term browser") {
		t.Fatal("term browser rendered without a term source")
	}
}

type fakeDocuments struct {
	page DocumentPage
	got  DocumentQuery
}

func (f *fakeDocuments) BrowseDocuments(_ context.Context, query DocumentQuery) DocumentPage {
	f.got = query

	return f.page
}

func TestConsoleIndexRendersDocumentBrowser(t *testing.T) {
	t.Parallel()

	docs := &fakeDocuments{page: DocumentPage{
		Documents: []DocumentSummary{{
			URL: "https://a.example/1", Title: "Doc One",
			ContentType: "text/html", Language: "en", IndexedAt: "2026-07-04T00:00:00Z",
		}},
		Matched: 60, Limit: 50, Truncated: true,
	}}
	console := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Documents: docs,
	})

	got := do(t, console, "/admin/index?q=example&domain=a.example")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if docs.got.URLContains != "example" || docs.got.Domain != "a.example" {
		t.Fatalf("filters not passed to source: %+v", docs.got)
	}
	for _, want := range []string{
		"Document browser", "https://a.example/1", "Doc One", "text/html",
		"60 matching document(s)", "showing the first 50",
		`value="example"`, `value="a.example"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("document browser missing %q", want)
		}
	}
}

func TestConsoleIndexDocumentBrowserEmptyState(t *testing.T) {
	t.Parallel()

	got := do(
		t,
		New(
			Options{
				Index:     fakeIndex{snap: IndexStats{Available: true}},
				Documents: &fakeDocuments{},
			},
		),
		"/admin/index",
	)
	if !strings.Contains(got.body, "No documents match.") {
		t.Fatal("expected the empty document-browser state")
	}
}

func TestConsoleIndexDocumentBrowserUnavailable(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Documents: &fakeDocuments{page: DocumentPage{ScanFailed: true}},
	})
	body := do(t, console, "/admin/index").body
	if !strings.Contains(body, "Document browser is unavailable.") {
		t.Fatal("document scan failure should render unavailable")
	}
	if strings.Contains(body, "No documents match.") {
		t.Fatal("document scan failure rendered an honest-empty state")
	}
}

func TestConsoleIndexDocumentBrowserLabelsPartialScanFailure(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Documents: &fakeDocuments{page: DocumentPage{
			Documents:  []DocumentSummary{{URL: "https://partial.example/1"}},
			Matched:    1,
			ScanFailed: true,
		}},
	})
	body := do(t, console, "/admin/index").body
	for _, want := range []string{
		"Document browser is unavailable.",
		"Showing 1 document(s) collected before the scan failed; this is a partial list.",
		"https://partial.example/1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial scan failure missing %q", want)
		}
	}
	if strings.Contains(body, "1 matching document(s)") {
		t.Fatal("partial scan failure rendered an exact match count")
	}
}

func TestConsoleIndexOmitsDocumentBrowserWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}}), "/admin/index")
	if strings.Contains(got.body, "Document browser") {
		t.Fatal("document browser rendered without a source")
	}
}

type fakeIndexAdmin struct {
	deletedKeys    []string
	deletedDomains []string
	err            error
}

func (f *fakeIndexAdmin) DeleteDocument(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)

	return f.err
}

func (f *fakeIndexAdmin) DeleteDomain(_ context.Context, domain string) (int, error) {
	f.deletedDomains = append(f.deletedDomains, domain)

	return len(f.deletedDomains), f.err
}

func docBrowserWithOne() *fakeDocuments {
	return &fakeDocuments{page: DocumentPage{
		Documents: []DocumentSummary{{URL: "https://a.example/1", Key: "https://a.example/1"}},
		Matched:   1,
		Limit:     50,
	}}
}

func TestConsoleIndexRendersDeleteControls(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index:      fakeIndex{snap: IndexStats{Available: true}},
		Documents:  docBrowserWithOne(),
		IndexAdmin: &fakeIndexAdmin{},
	})
	got := do(t, console, "/admin/index?domain=a.example")
	for _, want := range []string{
		"Delete all from a.example", `value="domain"`,
		">Delete<", `value="url"`, `value="https://a.example/1"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("delete controls missing %q", want)
		}
	}

	without := do(
		t,
		New(
			Options{
				Index:     fakeIndex{snap: IndexStats{Available: true}},
				Documents: docBrowserWithOne(),
			},
		),
		"/admin/index?domain=a.example",
	)
	if strings.Contains(without.body, ">Delete<") ||
		strings.Contains(without.body, "Delete all from") {
		t.Fatal("no delete controls should render without an index-admin source")
	}
}

func TestConsoleIndexDeleteDocument(t *testing.T) {
	t.Parallel()

	admin := &fakeIndexAdmin{}
	got := doPost(t, New(Options{IndexAdmin: admin}), indexDeletePath, url.Values{
		"action": {"url"}, "url": {"https://a.example/1"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if loc := got.header.Get("Location"); loc != indexPath {
		t.Fatalf("location %q, want %q", loc, indexPath)
	}
	if len(admin.deletedKeys) != 1 || admin.deletedKeys[0] != "https://a.example/1" {
		t.Fatalf("deleted keys = %v", admin.deletedKeys)
	}
}

func TestConsoleIndexDeleteDomain(t *testing.T) {
	t.Parallel()

	admin := &fakeIndexAdmin{}
	got := doPost(t, New(Options{IndexAdmin: admin}), indexDeletePath, url.Values{
		"action": {"domain"}, "domain": {"a.example"},
	})
	if got.status != http.StatusSeeOther || len(admin.deletedDomains) != 1 {
		t.Fatalf("status %d, domains %v", got.status, admin.deletedDomains)
	}
}

func TestConsoleIndexDeleteRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{IndexAdmin: &fakeIndexAdmin{}}), indexDeletePath, url.Values{
		"action": {"purge"},
	})
	if got.status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", got.status)
	}
}

func TestConsoleIndexDeleteWithoutSourceIsNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), indexDeletePath, url.Values{"action": {"url"}, "url": {"x"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}

func TestConsoleIndexDeleteRedirectsOnError(t *testing.T) {
	t.Parallel()

	admin := &fakeIndexAdmin{err: errors.New("delete failed")}
	got := doPost(t, New(Options{IndexAdmin: admin}), indexDeletePath, url.Values{
		"action": {"url"}, "url": {"https://a.example/1"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("a failed delete should still redirect: status %d", got.status)
	}
}

type fakeBlacklist struct {
	entries []BlacklistEntry
	added   []string
	removed []string
	readErr error
	err     error
}

func (f *fakeBlacklist) BlacklistEntries(
	context.Context,
) ([]BlacklistEntry, error) {
	return f.entries, f.readErr
}

func (f *fakeBlacklist) AddBlacklist(_ context.Context, kind, value string) error {
	f.added = append(f.added, kind+":"+value)

	return f.err
}

func (f *fakeBlacklist) RemoveBlacklist(_ context.Context, kind, value string) error {
	f.removed = append(f.removed, kind+":"+value)

	return f.err
}

func TestConsoleIndexRendersBlacklist(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: &fakeBlacklist{entries: []BlacklistEntry{
			{Kind: "domain", Value: "blocked.example", AddedAt: "2026-07-05T00:00:00Z"},
		}},
	})
	got := do(t, console, "/admin/index")
	for _, want := range []string{
		"Blacklist", "Block a URL or a whole domain", `value="add"`,
		"blocked.example", `value="remove"`, ">Remove<",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("blacklist manager missing %q", want)
		}
	}

	empty := do(t, New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: &fakeBlacklist{},
	}), "/admin/index")
	if !strings.Contains(empty.body, "The blacklist is empty.") {
		t.Fatal("expected the empty blacklist state")
	}

	without := do(
		t,
		New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}}),
		"/admin/index",
	)
	if strings.Contains(without.body, "Block a URL or a whole domain") {
		t.Fatal("no blacklist manager should render without a source")
	}
}

func TestConsoleIndexBlacklistReadFailureIsNotEmpty(t *testing.T) {
	t.Parallel()

	body := do(t, New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: &fakeBlacklist{
			readErr: errors.New("read failed"),
		},
	}), "/admin/index").body
	if !strings.Contains(body, "Blacklist entries are unavailable.") {
		t.Fatal("blacklist read failure should render unavailable")
	}
	if strings.Contains(body, "The blacklist is empty.") {
		t.Fatal("blacklist read failure rendered an honest-empty state")
	}
	for _, want := range []string{">Block<", ">Check<", ">Export<", ">Import<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("blacklist read failure hid control %q", want)
		}
	}
}

func TestConsoleBlacklistAdd(t *testing.T) {
	t.Parallel()

	blacklist := &fakeBlacklist{}
	got := doPost(t, New(Options{Blacklist: blacklist}), blacklistPath, url.Values{
		"action": {"add"}, "kind": {"domain"}, "value": {"blocked.example"},
	})
	if got.status != http.StatusSeeOther || got.header.Get("Location") != indexPath {
		t.Fatalf("status %d, location %q", got.status, got.header.Get("Location"))
	}
	if len(blacklist.added) != 1 || blacklist.added[0] != "domain:blocked.example" {
		t.Fatalf("added = %v", blacklist.added)
	}
}

func TestConsoleBlacklistRemove(t *testing.T) {
	t.Parallel()

	blacklist := &fakeBlacklist{}
	got := doPost(t, New(Options{Blacklist: blacklist}), blacklistPath, url.Values{
		"action": {"remove"}, "kind": {"url"}, "value": {"https://a.example/"},
	})
	if got.status != http.StatusSeeOther || len(blacklist.removed) != 1 {
		t.Fatalf("status %d, removed %v", got.status, blacklist.removed)
	}
}

func TestConsoleBlacklistRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{Blacklist: &fakeBlacklist{}}), blacklistPath, url.Values{
		"action": {"purge"},
	})
	if got.status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", got.status)
	}
}

func TestConsoleBlacklistWithoutSourceIsNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), blacklistPath, url.Values{"action": {"add"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}

func TestConsoleBlacklistRedirectsOnError(t *testing.T) {
	t.Parallel()

	blacklist := &fakeBlacklist{err: errors.New("write failed")}
	got := doPost(t, New(Options{Blacklist: blacklist}), blacklistPath, url.Values{
		"action": {"add"}, "kind": {"domain"}, "value": {"blocked.example"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("a failed blacklist write should still redirect: status %d", got.status)
	}
}
