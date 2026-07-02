package compatibility

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yacyproto"
)

func TestCatalogIncludesCurrentPeerProtocolSurfaces(t *testing.T) {
	report := Catalog()
	paths := map[string]Surface{}
	for _, surface := range report.Surfaces {
		paths[surface.Path] = surface
	}

	for _, path := range []string{
		yacyproto.PathHello,
		yacyproto.PathQuery,
		yacyproto.PathTransferRWI,
		yacyproto.PathTransferURL,
		yacyproto.PathSearch,
		yacyproto.PathSeedlist,
		yacyproto.PathSeedlistJSON,
		yacyproto.PathSeedlistXML,
		yacyproto.PathCrawlURLs,
		yacyproto.PathCrawlReceipt,
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("catalog missing %s", path)
		}
	}
	if paths[yacyproto.PathTransferRWI].State != Implemented ||
		!slices.Contains(paths[yacyproto.PathTransferRWI].Methods, "POST") {
		t.Fatalf("transferRWI surface = %#v", paths[yacyproto.PathTransferRWI])
	}
	if paths[yacyproto.PathCrawlURLs].State != Partial {
		t.Fatalf("crawl urls state = %q, want partial", paths[yacyproto.PathCrawlURLs].State)
	}
}

func TestCatalogIncludesPlannedCompatibilityGaps(t *testing.T) {
	report := Catalog()
	paths := map[string]Surface{}
	for _, surface := range report.Surfaces {
		paths[surface.Path] = surface
	}

	for _, path := range []string{"/solr/select", "/gsa/searchresult", "/extract"} {
		if got := paths[path]; got.State != Planned {
			t.Fatalf("%s state = %q, want planned", path, got.State)
		}
	}
	if got := paths["/search"]; got.State != Partial {
		t.Fatalf("/search state = %q, want partial", got.State)
	}
	if got := paths["/*_p.html"]; got.State != Unsupported {
		t.Fatalf("admin clone state = %q, want unsupported", got.State)
	}
	if got := paths["/solr/*"]; got.State != Unsupported {
		t.Fatalf("solr api state = %q, want unsupported", got.State)
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
