package compatibility

import (
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestCatalogIncludesCurrentPeerProtocolSurfaces(t *testing.T) {
	report := Catalog()
	paths := map[string]Surface{}
	for _, surface := range report.Surfaces {
		paths[surface.Path] = surface
	}

	for _, path := range []string{
		yagoproto.PathHello,
		yagoproto.PathQuery,
		yagoproto.PathTransferRWI,
		yagoproto.PathTransferURL,
		yagoproto.PathSearch,
		yagoproto.PathSeedlist,
		yagoproto.PathSeedlistJSON,
		yagoproto.PathSeedlistXML,
		yagoproto.PathCrawlURLs,
		yagoproto.PathCrawlReceipt,
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("catalog missing %s", path)
		}
	}
	if paths[yagoproto.PathTransferRWI].State != Implemented ||
		!slices.Contains(paths[yagoproto.PathTransferRWI].Methods, "POST") {
		t.Fatalf("transferRWI surface = %#v", paths[yagoproto.PathTransferRWI])
	}
	if !strings.Contains(paths[yagoproto.PathTransferRWI].Behavior, "1,000") ||
		!strings.Contains(paths[yagoproto.PathTransferRWI].Behavior, "too high load") ||
		!strings.Contains(paths[yagoproto.PathTransferRWI].Behavior, "busy") ||
		!strings.Contains(paths[yagoproto.PathTransferRWI].Behavior, "millisecond pause") {
		t.Fatalf("transferRWI bounds = %#v", paths[yagoproto.PathTransferRWI])
	}
	if !strings.Contains(paths[yagoproto.PathSearch].Behavior, "wi WordReferenceRow") {
		t.Fatalf("remote search interop = %#v", paths[yagoproto.PathSearch])
	}
	for _, path := range []string{
		yagoproto.PathTransferRWI,
		yagoproto.PathTransferURL,
		yagoproto.PathSearch,
	} {
		if !strings.Contains(paths[path].Notes, "default freeworld and same-name") ||
			!strings.Contains(
				paths[path].Notes,
				"salted-magic-sim with one nonempty shared secret",
			) {
			t.Fatalf("%s authentication scope = %#v", path, paths[path])
		}
	}
	if paths[yagoproto.PathCrawlURLs].State != Partial {
		t.Fatalf("crawl urls state = %q, want partial", paths[yagoproto.PathCrawlURLs].State)
	}
	if !strings.Contains(paths[yagoproto.PathCrawlURLs].Behavior, "durable single-URL") ||
		!strings.Contains(paths[yagoproto.PathCrawlURLs].Behavior, "salted-magic-sim") {
		t.Fatalf("remote crawl URL policy = %#v", paths[yagoproto.PathCrawlURLs])
	}
	if !strings.Contains(paths[yagoproto.PathCrawlReceipt].Behavior, "delay 3600") ||
		!strings.Contains(paths[yagoproto.PathCrawlReceipt].Behavior, "returns 9999") ||
		!strings.Contains(paths[yagoproto.PathCrawlReceipt].Behavior, "returns 10") ||
		!strings.Contains(paths[yagoproto.PathCrawlReceipt].Behavior, "cannot create or extend") {
		t.Fatalf("crawl receipt delay = %#v", paths[yagoproto.PathCrawlReceipt])
	}
}

func TestCatalogDescribesCurrentTavilyContract(t *testing.T) {
	report := Catalog()
	wantBehavior := map[string][]string{
		"/search":  {"canonical root-portal order", "errors carry only detail.error"},
		"/extract": {"at most 20 URLs", "raw-scope authentication"},
		"/crawl":   {"200-page", "30-second"},
		"/map":     {"without page content", "raw-work limits"},
	}
	for _, surface := range report.Surfaces {
		if surface.Area != areaAgentAPI {
			continue
		}
		phrases, ok := wantBehavior[surface.Path]
		if !ok {
			continue
		}
		for _, phrase := range phrases {
			if !strings.Contains(surface.Behavior, phrase) {
				t.Fatalf(
					"%s behavior %q does not contain %q",
					surface.Path,
					surface.Behavior,
					phrase,
				)
			}
		}
		delete(wantBehavior, surface.Path)
	}
	if len(wantBehavior) != 0 {
		t.Fatalf("catalog is missing Tavily surfaces: %v", wantBehavior)
	}
}

func TestCatalogDescribesRequestLocalTavilyUsage(t *testing.T) {
	report := Catalog()
	wantBehavior := map[string][]string{
		"/search":  {"request-local compatible unit", "executed advanced search"},
		"/extract": {"complete groups of five", "successful extractions"},
		"/crawl":   {"complete groups of ten", "complete groups of five"},
		"/map":     {"complete groups of ten", "successful pages"},
	}
	for _, surface := range report.Surfaces {
		phrases, ok := wantBehavior[surface.Path]
		if !ok || surface.Area != areaAgentAPI {
			continue
		}
		for _, phrase := range phrases {
			if !strings.Contains(surface.Behavior, phrase) {
				t.Fatalf(
					"%s behavior %q does not contain %q",
					surface.Path,
					surface.Behavior,
					phrase,
				)
			}
		}
		if !strings.Contains(surface.Notes, "not billing") &&
			!strings.Contains(surface.Notes, "rather than billing") {
			t.Fatalf(
				"%s notes do not distinguish usage from billing: %q",
				surface.Path,
				surface.Notes,
			)
		}
		if strings.Contains(surface.Notes, "real credit accounting") ||
			strings.Contains(surface.Notes, "synthetic zero") {
			t.Fatalf("%s retains superseded usage limitation: %q", surface.Path, surface.Notes)
		}
		delete(wantBehavior, surface.Path)
	}
	if len(wantBehavior) != 0 {
		t.Fatalf("catalog is missing Tavily usage surfaces: %v", wantBehavior)
	}
}

func TestCatalogIncludesPlannedCompatibilityGaps(t *testing.T) {
	report := Catalog()
	paths := map[string]Surface{}
	for _, surface := range report.Surfaces {
		paths[surface.Path] = surface
	}

	if got := paths["/gsa/searchresult"]; got.State != Unsupported {
		t.Fatalf("gsa state = %q, want unsupported (removed upstream)", got.State)
	}
	for _, path := range []string{"/search", "/extract", "/crawl", "/map"} {
		if got := paths[path]; got.State != Partial {
			t.Fatalf("%s state = %q, want partial", path, got.State)
		}
	}
	if got := paths["/*_p.html"]; got.State != Unsupported {
		t.Fatalf("admin clone state = %q, want unsupported", got.State)
	}
	for _, path := range []string{"/solr/select", "/solr/*"} {
		if got := paths[path]; got.State != Unsupported {
			t.Fatalf("%s state = %q, want unsupported", path, got.State)
		}
	}
}

func TestCatalogIncludesCurrentAdminSurfaces(t *testing.T) {
	report := Catalog()
	paths := map[string]Surface{}
	for _, surface := range report.Surfaces {
		paths[surface.Path] = surface
	}

	for _, path := range []string{
		"/health",
		"/ready",
		"/metrics",
		"/api/admin/v1/compatibility",
		"/api/admin/v1/network/dht/gates",
		"/api/admin/v1/index/stats",
		"/api/admin/v1/search/ranking/trust",
	} {
		if got := paths[path]; got.State != Implemented {
			t.Fatalf("%s state = %q, want implemented", path, got.State)
		}
	}
	if got := paths["/crawl"]; got.State != Partial {
		t.Fatalf("/crawl state = %q, want partial", got.State)
	}
}

func TestCatalogCountsMatchSurfaces(t *testing.T) {
	report := Catalog()
	totals := map[State]int{}
	for _, surface := range report.Surfaces {
		totals[surface.State]++
	}

	for _, count := range report.Counts {
		if totals[count.State] != count.Total {
			t.Fatalf("count %q = %d, want %d", count.State, count.Total, totals[count.State])
		}
		delete(totals, count.State)
	}
	for state, total := range totals {
		if total != 0 {
			t.Fatalf("missing count for %q=%d", state, total)
		}
	}
}

func TestCatalogReturnsCopies(t *testing.T) {
	first := Catalog()
	first.Surfaces[0].Path = "mutated"
	first.Surfaces[0].Methods[0] = "MUTATED"
	first.Surfaces[0].Evidence[0] = "mutated"

	second := Catalog()
	if second.Surfaces[0].Path == "mutated" ||
		second.Surfaces[0].Methods[0] == "MUTATED" ||
		second.Surfaces[0].Evidence[0] == "mutated" {
		t.Fatalf("catalog leaked mutable state: %#v", second.Surfaces[0])
	}
}
