package tavilyapi

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRequestFirstSeenBoundsStayExplicit(t *testing.T) {
	days := 5

	start, end := requestFirstSeenBounds(SearchRequest{
		FirstSeenStart: "2026-01-01", FirstSeenEnd: "2026-02-01",
	})
	if start != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("first-seen start = %v", start)
	}
	// The end bound is inclusive to the last representable instant of the end
	// day. Anything coarser silently excludes a document first seen inside that
	// final second, which is the day's busiest for a node that crawls
	// continuously, so the widening is pinned at the granularity time.Time keeps.
	if want := time.Date(2026, 2, 1, 23, 59, 59, 999_999_999, time.UTC); !end.Equal(want) {
		t.Fatalf("first-seen end = %v, want the end day's last nanosecond %v", end, want)
	}

	// A start alone leaves the end open.
	if _, end = requestFirstSeenBounds(
		SearchRequest{FirstSeenStart: "2026-01-01"},
	); !end.IsZero() {
		t.Fatalf("open first-seen end = %v", end)
	}
	if start, end = requestFirstSeenBounds(SearchRequest{}); !start.IsZero() || !end.IsZero() {
		t.Fatalf("absent first-seen window = %v %v", start, end)
	}

	// The publication-recency vocabulary keeps its upstream meaning: none of it
	// produces a first-seen bound.
	for name, req := range map[string]SearchRequest{
		"time_range":      {TimeRange: "week"},
		"days":            {Days: &days},
		"news topic":      {Topic: "news"},
		"explicit window": {StartDate: "2026-01-01", EndDate: "2026-02-01"},
	} {
		if start, end = requestFirstSeenBounds(req); !start.IsZero() || !end.IsZero() {
			t.Fatalf("%s produced a first-seen window: %v %v", name, start, end)
		}
	}
	// And a first-seen window never bounds the publication date.
	if start, end = requestTimeBounds(SearchRequest{
		FirstSeenStart: "2026-01-01", FirstSeenEnd: "2026-02-01",
	}); !start.IsZero() || !end.IsZero() {
		t.Fatalf("first-seen window produced document-date bounds: %v %v", start, end)
	}
}

func TestValidateFirstSeenRange(t *testing.T) {
	if err := validateFirstSeenRange("2026-01-01", "2026-02-01"); err != nil {
		t.Fatalf("valid window refused: %v", err)
	}
	if err := validateFirstSeenRange("", ""); err != nil {
		t.Fatalf("absent window refused: %v", err)
	}
	// An open-ended window is legal in both directions and must survive the
	// reversed-range refusal, which compares an absent bound against a set one.
	if err := validateFirstSeenRange("2026-01-01", ""); err != nil {
		t.Fatalf("open-ended first-seen window refused: %v", err)
	}
	if err := validateFirstSeenRange("", "2026-02-01"); err != nil {
		t.Fatalf("open-started first-seen window refused: %v", err)
	}
	if err := validateFirstSeenRange("2026-1-1", ""); err == nil {
		t.Fatal("malformed first_seen_start must be refused")
	} else if err.Error() != "first_seen_start must use YYYY-MM-DD" {
		t.Fatalf("start message = %q", err.Error())
	}
	if err := validateFirstSeenRange("", "2026-2-1"); err == nil {
		t.Fatal("malformed first_seen_end must be refused")
	} else if err.Error() != "first_seen_end must use YYYY-MM-DD" {
		t.Fatalf("end message = %q", err.Error())
	}
	if err := validateFirstSeenRange("2026-02-02", "2026-02-01"); err == nil {
		t.Fatal("a reversed first-seen window must be refused")
	} else if err.Error() != "first_seen_start must not be after first_seen_end" {
		t.Fatalf("order message = %q", err.Error())
	}
}

func TestSearchAppliesFirstSeenBoundsToResults(t *testing.T) {
	endpoint, search, _ := firstSeenSearchEndpoint()

	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{
			Query:          "golang",
			FirstSeenStart: "2026-07-01",
			FirstSeenEnd:   "2026-07-31",
		},
		time.Unix(100, 0),
		"id-1",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Rows this node does not hold carry no first-seen time, so they cannot be
	// shown to fall inside the window and drop with it.
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/doc" {
		t.Fatalf("results = %+v, want only the locally held row", resp.Results)
	}
	if !search.got.MinFirstSeen.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("core min first seen = %v", search.got.MinFirstSeen)
	}
	wantEnd := time.Date(2026, 7, 31, 23, 59, 59, 999_999_999, time.UTC)
	if !search.got.MaxFirstSeen.Equal(wantEnd) {
		t.Fatalf("core max first seen = %v, want %v", search.got.MaxFirstSeen, wantEnd)
	}
	// The first-seen window is not a publication window.
	if !search.got.MinDate.IsZero() || !search.got.MaxDate.IsZero() {
		t.Fatalf("core date bounds = %v %v", search.got.MinDate, search.got.MaxDate)
	}
}

// TestSearchAppliesAnEndOnlyFirstSeenWindow pins the half of the window a closed
// window hides. "Everything this node discovered up to a day" is a legal request
// on its own, and it filters exactly as a start-only window does: a row this
// node does not hold carries no first-seen time and cannot be shown to fall
// inside it.
func TestSearchAppliesAnEndOnlyFirstSeenWindow(t *testing.T) {
	endpoint, search, _ := firstSeenSearchEndpoint()

	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", FirstSeenEnd: "2026-07-31"},
		time.Unix(100, 0),
		"id-3",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/doc" {
		t.Fatalf("results = %+v, want only the locally held row", resp.Results)
	}
	if !search.got.MinFirstSeen.IsZero() {
		t.Fatalf("core min first seen = %v, want an open start", search.got.MinFirstSeen)
	}
	wantEnd := time.Date(2026, 7, 31, 23, 59, 59, 999_999_999, time.UTC)
	if !search.got.MaxFirstSeen.Equal(wantEnd) {
		t.Fatalf("core max first seen = %v, want %v", search.got.MaxFirstSeen, wantEnd)
	}
}

// TestSearchWithoutFirstSeenBoundsKeepsRemoteResults is the other half of the
// guard above: without a first-seen window, peer and web rows are served.
func TestSearchWithoutFirstSeenBoundsKeepsRemoteResults(t *testing.T) {
	endpoint, search, _ := firstSeenSearchEndpoint()

	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang"},
		time.Unix(100, 0),
		"id-2",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("results = %+v, want every row served", resp.Results)
	}
	if !search.got.MinFirstSeen.IsZero() || !search.got.MaxFirstSeen.IsZero() {
		t.Fatalf(
			"core first-seen bounds = %v %v",
			search.got.MinFirstSeen,
			search.got.MaxFirstSeen,
		)
	}
}

// firstSeenSearchEndpoint answers with one locally held row, one peer row, and
// one web row, so a first-seen window has something of each kind to decide on.
func firstSeenSearchEndpoint() (searchEndpoint, *fakeSearcher, *fakeDocuments) {
	endpoint, search, documents := richSearchEndpoint()
	search.response.Results[0].Source = searchcore.SourceGlobal
	search.response.Results[1] = searchcore.Result{
		Title:   "Peer row",
		URL:     "https://peer.example/doc",
		Snippet: "peer snippet",
		Score:   2,
		Host:    "peer.example",
		Source:  searchcore.SourceRemote,
	}
	search.response.Results = append(search.response.Results, searchcore.Result{
		Title:   "Web row",
		URL:     "https://web.example/doc",
		Snippet: "web snippet",
		Score:   1,
		Host:    "web.example",
		Source:  searchcore.SourceWeb,
	})

	return endpoint, search, documents
}

// TestDocumentDateBoundsKeepRemoteRows pins the conjunct that makes the
// first-seen refusal conditional. Dropping a peer or web row because this node
// does not hold it is correct only while a first-seen bound is active; without
// that guard the same line fires under an ordinary published_date window and
// silently strips every remote and web answer from the response. The
// early return above the loop hides it -- a request with no bound at all never
// reaches the line -- so the case has to carry a document-date bound and a
// remote row that satisfies it.
func TestDocumentDateBoundsKeepRemoteRows(t *testing.T) {
	t.Parallel()

	window := searchcore.Request{
		MinDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxDate: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	if window.FirstSeenBounded() {
		t.Fatal("the fixture must not carry a first-seen bound")
	}
	results := []searchcore.Result{
		{URL: "https://local.example", Date: "20260615", Source: searchcore.SourceGlobal},
		{URL: "https://peer.example", Date: "2026-06-15", Source: searchcore.SourceRemote},
		{URL: "https://web.example", Date: "20260615", Source: searchcore.SourceWeb},
	}

	kept := resultsWithinRequestedBounds(results, window)
	if len(kept) != len(results) {
		t.Fatalf("a published-date window kept %d of %d rows; remote and web answers "+
			"must survive it", len(kept), len(results))
	}

	// With the bound active the same rows must lose their remote and web
	// members, which is what makes the guard above meaningful rather than dead.
	window.MinFirstSeen = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	kept = resultsWithinRequestedBounds(results, window)
	if len(kept) != 1 || kept[0].URL != "https://local.example" {
		t.Fatalf("first-seen window kept %+v, want only the locally stored row", kept)
	}
}
