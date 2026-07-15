package adminui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// branchBlacklist is a denylist double whose probe, export, and import operations
// fail on demand, exercising the console's error branches. A zero value succeeds.
type branchBlacklist struct {
	probeErr  error
	exportErr error
	importErr error
}

func (branchBlacklist) BlacklistEntries(context.Context) ([]BlacklistEntry, error) {
	return nil, nil
}

func (branchBlacklist) AddBlacklist(context.Context, string, string) error { return nil }

func (branchBlacklist) RemoveBlacklist(context.Context, string, string) error { return nil }

func (b branchBlacklist) BlacklistBlocks(context.Context, string) (bool, error) {
	return false, b.probeErr
}

func (b branchBlacklist) ExportBlacklist(context.Context) (string, error) {
	return "", b.exportErr
}

func (b branchBlacklist) ImportBlacklist(context.Context, string) (int, error) {
	return 1, b.importErr
}

// branchExporter is an index-export double that fails after the console has
// already committed the download headers.
type branchExporter struct{ err error }

func (b branchExporter) ExportDocuments(context.Context, IndexExportRequest, io.Writer) error {
	return b.err
}

// TestBranchBlacklistProbeVerdicts covers handleBlacklistTest's empty-URL prompt
// and its probe-error verdict.
func TestBranchBlacklistProbeVerdicts(t *testing.T) {
	t.Parallel()

	empty := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: &fakePorterBlacklist{},
	})
	if got := do(t, empty, blacklistTestPath+"?url="); !strings.Contains(
		got.body, "Enter a URL to test.",
	) {
		t.Fatalf("empty-url probe missing prompt: %.150q", got.body)
	}

	failing := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: branchBlacklist{probeErr: errors.New("prober offline")},
	})
	got := do(t, failing, blacklistTestPath+"?url="+url.QueryEscape("https://x.example/"))
	if !strings.Contains(got.body, "Probe failed:") {
		t.Fatalf("probe-error verdict missing: %.150q", got.body)
	}
}

// TestBranchBlacklistExportError covers handleBlacklistExport's failure path.
func TestBranchBlacklistExportError(t *testing.T) {
	t.Parallel()

	console := New(Options{Blacklist: branchBlacklist{exportErr: errors.New("dump failed")}})
	got := do(t, console, blacklistExportPath)
	if got.status != http.StatusInternalServerError {
		t.Fatalf("export error status = %d, want 500", got.status)
	}
}

// TestBranchBlacklistImport covers handleBlacklistImport's failure note and its
// no-source 404.
func TestBranchBlacklistImport(t *testing.T) {
	t.Parallel()

	failing := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Blacklist: branchBlacklist{importErr: errors.New("store locked")},
	})
	got := doPost(t, failing, blacklistImportPath, url.Values{"payload": {"domain a.example"}})
	if !strings.Contains(got.body, "then failed") {
		t.Fatalf("import failure note missing: %.150q", got.body)
	}

	none := doPost(t, New(Options{}), blacklistImportPath, url.Values{"payload": {"x"}})
	if none.status != http.StatusNotFound {
		t.Fatalf("import without a source = %d, want 404", none.status)
	}
}

// TestBranchIndexExportWriterError covers handleIndexExport's post-header error
// branch: the response has already committed 200 before the writer fails.
func TestBranchIndexExportWriterError(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index:       fakeIndex{snap: IndexStats{Available: true}},
		IndexExport: branchExporter{err: errors.New("stream broke")},
	})
	if got := do(t, console, indexExportPath+"?format=csv"); got.status != http.StatusOK {
		t.Fatalf("export writer error status = %d, want 200", got.status)
	}
}

// TestBranchExportContentMeta covers every arm of the format-to-headers map.
func TestBranchExportContentMeta(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		format, contentType, filename string
		ok                            bool
	}{
		{"", "text/plain; charset=utf-8", "index-urls.txt", true},
		{"text", "text/plain; charset=utf-8", "index-urls.txt", true},
		{"csv", "text/csv; charset=utf-8", "index-export.csv", true},
		{"jsonl", "application/x-ndjson", "index-export.jsonl", true},
		{"xml", "", "", false},
	} {
		ct, fn, ok := exportContentMeta(tc.format)
		if ct != tc.contentType || fn != tc.filename || ok != tc.ok {
			t.Errorf("exportContentMeta(%q) = %q, %q, %v", tc.format, ct, fn, ok)
		}
	}
}

// TestBranchCrawlScheduleEnable covers handleCrawlSchedule's enable action.
func TestBranchCrawlScheduleEnable(t *testing.T) {
	t.Parallel()

	source := &fakeScheduleSource{}
	got := doPost(t, New(Options{Schedules: source}), crawlSchedulePath, url.Values{
		"action": {"enable"}, "id": {"docs"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("enable status = %d, want 303", got.status)
	}
	if !source.toggled["docs"] {
		t.Fatalf("enable should set the schedule enabled: %v", source.toggled)
	}
}

// TestBranchPeerComparatorTieBreaks covers the hash tie-break in the numeric and
// timestamp comparators, where the sorted field is equal.
func TestBranchPeerComparatorTieBreaks(t *testing.T) {
	t.Parallel()

	rwiLess := peerIntLess(func(p NetworkPeer) int { return p.RWICount })
	timeLess := peerTimeLess(func(p NetworkPeer) time.Time { return p.LastSeenAt })
	stamp := time.Unix(1_700_000_000, 0)

	for _, tc := range []struct {
		name string
		got  bool
		want bool
	}{
		{
			"equal rwi orders by hash ascending",
			rwiLess(
				NetworkPeer{Hash: "A", RWICount: 7},
				NetworkPeer{Hash: "B", RWICount: 7},
			),
			true,
		},
		{
			"equal rwi reversed hash",
			rwiLess(
				NetworkPeer{Hash: "B", RWICount: 7},
				NetworkPeer{Hash: "A", RWICount: 7},
			),
			false,
		},
		{
			"equal last-seen orders by hash",
			timeLess(
				NetworkPeer{Hash: "A", LastSeenAt: stamp},
				NetworkPeer{Hash: "B", LastSeenAt: stamp},
			),
			true,
		},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBranchPeerAddressLess covers the equal-IP hash tie-break and the
// both-hostnames case-folded fallback in peerAddressLess.
func TestBranchPeerAddressLess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  bool
		want bool
	}{
		{
			"equal IPs fall back to hash order",
			peerAddressLess(
				NetworkPeer{Hash: "A", Address: "10.0.0.1:8090"},
				NetworkPeer{Hash: "B", Address: "10.0.0.1:9090"},
			),
			true,
		},
		{
			"two hostnames compare case-folded",
			peerAddressLess(
				NetworkPeer{Hash: "Z", Address: "Alpha.example"},
				NetworkPeer{Hash: "A", Address: "beta.example"},
			),
			true,
		},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBranchColumnAriaSortAscending covers ColumnAriaSort's ascending arm.
func TestBranchColumnAriaSortAscending(t *testing.T) {
	t.Parallel()

	active := PeerTableView{SortKey: "name", SortDir: "asc"}
	if got := active.ColumnAriaSort("name"); got != "ascending" {
		t.Fatalf("ColumnAriaSort on an ascending column = %q, want ascending", got)
	}
}
