package searchindex

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestAllowsFirstSeenBounds(t *testing.T) {
	firstSeen := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	// A download page with no publication date still has a first-seen time.
	seen := documentstore.Document{
		NormalizedURL: "https://a.example/download",
		FirstSeenAt:   firstSeen,
	}
	unseen := documentstore.Document{NormalizedURL: "https://a.example/legacy"}

	if !allowsFirstSeen(seen, SearchRequest{}) {
		t.Fatal("no bounds must pass")
	}
	if !allowsFirstSeen(unseen, SearchRequest{}) {
		t.Fatal("no bounds must pass a document with no first-seen time")
	}
	if !allowsFirstSeen(seen, SearchRequest{
		MinFirstSeen: firstSeen.Add(-time.Hour),
		MaxFirstSeen: firstSeen.Add(time.Hour),
	}) {
		t.Fatal("first-seen time inside the window must pass")
	}
	if allowsFirstSeen(seen, SearchRequest{MinFirstSeen: firstSeen.Add(time.Hour)}) {
		t.Fatal("first-seen time before the window must drop")
	}
	if allowsFirstSeen(seen, SearchRequest{MaxFirstSeen: firstSeen.Add(-time.Hour)}) {
		t.Fatal("first-seen time after the window must drop")
	}
	// Both bounds are inclusive: the exact boundary qualifies, one step past it
	// does not.
	if !allowsFirstSeen(seen, SearchRequest{MinFirstSeen: firstSeen}) {
		t.Fatal("first-seen time exactly at the start bound must pass")
	}
	if allowsFirstSeen(seen, SearchRequest{MinFirstSeen: firstSeen.Add(time.Nanosecond)}) {
		t.Fatal("first-seen time one step before the start bound must drop")
	}
	if !allowsFirstSeen(seen, SearchRequest{MaxFirstSeen: firstSeen}) {
		t.Fatal("first-seen time exactly at the end bound must pass")
	}
	if allowsFirstSeen(seen, SearchRequest{MaxFirstSeen: firstSeen.Add(-time.Nanosecond)}) {
		t.Fatal("first-seen time one step after the end bound must drop")
	}
	if allowsFirstSeen(unseen, SearchRequest{MinFirstSeen: firstSeen}) {
		t.Fatal("no first-seen time must drop under an active start bound")
	}
	if allowsFirstSeen(unseen, SearchRequest{MaxFirstSeen: firstSeen}) {
		t.Fatal("no first-seen time must drop under an active end bound")
	}
}

// TestFirstSeenBoundsAreIndependentOfPublication pins the reason this filter
// exists: it answers "when did this node first see the page", so it selects
// pages that carry no publication date at all, and a publication-date bound
// never selects by first-seen time.
func TestFirstSeenBoundsAreIndependentOfPublication(t *testing.T) {
	undatedButSeen := documentstore.Document{
		NormalizedURL: "https://a.example/download",
		FirstSeenAt:   time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
	publishedLongAgo := documentstore.Document{
		NormalizedURL:  "https://a.example/paper",
		FetchedAt:      time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		PublishedAt:    time.Date(2015, 3, 1, 0, 0, 0, 0, time.UTC),
		DateConfidence: 1,
		FirstSeenAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
	window := SearchRequest{MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)}

	if !allowsFirstSeen(undatedButSeen, window) {
		t.Fatal("an undated page first seen inside the window must pass")
	}
	if allowsDocumentDate(
		undatedButSeen,
		SearchRequest{MinDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	) {
		t.Fatal("an undated page must still drop under a publication-date bound")
	}
	if !allowsFirstSeen(publishedLongAgo, window) {
		t.Fatal("a recently discovered old page must pass a recent first-seen window")
	}
	if allowsDocumentDate(
		publishedLongAgo,
		SearchRequest{MinDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	) {
		t.Fatal("a page published in 2015 must drop under a 2026 publication bound")
	}
}

func TestSearchAppliesFirstSeenBounds(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: firstSeenBoundDocuments(),
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	recent, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 5,
		MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if recent.Total != 1 || len(recent.Results) != 1 ||
		recent.Results[0].URL != "https://a.example/download" {
		t.Fatalf("first-seen bounded results = %+v", recent)
	}
	older, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 5,
		MaxFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if older.Total != 1 || len(older.Results) != 1 ||
		older.Results[0].URL != "https://a.example/archive" {
		t.Fatalf("first-seen end-bounded results = %+v", older)
	}
	// Absent bounds filter nothing, even though neither page is dated.
	all, err := index.Search(t.Context(), SearchRequest{Query: "golang", MaxResults: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if all.Total != 2 || len(all.Results) != 2 {
		t.Fatalf("unbounded results = %+v", all)
	}
}

// TestFirstSeenBoundsForcePostFilters pins the disk backend's honest-total
// contract: a first-seen bound can only be answered from the stored document, so
// it has to keep the hit loop hydrating past a full page.
func TestFirstSeenBoundsForcePostFilters(t *testing.T) {
	if hasPostFilters(SearchRequest{Query: "golang"}) {
		t.Fatal("an unfiltered request must not force post-filtering")
	}
	if !hasPostFilters(SearchRequest{
		MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}) {
		t.Fatal("a first-seen start bound must force post-filtering")
	}
	if !hasPostFilters(SearchRequest{
		MaxFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}) {
		t.Fatal("a first-seen end bound must force post-filtering")
	}
}

func TestBleveDiskSearchCountsFirstSeenBoundedHitsHonestly(t *testing.T) {
	documents := firstSeenBoundDocuments()
	index, err := NewBleveDiskIndex(
		t.Context(), filepath.Join(t.TempDir(), "first-seen.bleve"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })

	// One page per requested page of results, so an unhydrated tail would be
	// counted from the bleve total instead of from what the bound admits.
	recent, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 1,
		MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if recent.Total != 1 || len(recent.Results) != 1 ||
		recent.Results[0].URL != "https://a.example/download" {
		t.Fatalf("first-seen bounded disk results = %#v", recent)
	}
	unbounded, err := index.Search(t.Context(), SearchRequest{Query: "golang", MaxResults: 1})
	if err != nil {
		t.Fatal(err)
	}
	if unbounded.Total != 2 || len(unbounded.Results) != 1 {
		t.Fatalf("unbounded disk results = %#v", unbounded)
	}
}

// TestBleveDiskCandidateSearchAppliesFirstSeenBounds runs the disk backend in
// the only configuration a live node ever uses. searchlocal sets CandidateOnly
// on every local query, because the cached index it wraps satisfies
// SearchEvidenceSource and the candidate union asserts exactly that, and there
// is no switch that turns it off. A first-seen bound additionally forces the
// complete-hits scan, so the live bounded path hydrates each hit from the stored
// projection and never from the document vault.
//
// That is why a projection missing the first-seen time was invisible: it read
// the field as the zero time, and the zero refuses under an active bound, so
// every bounded query on the production node returned nothing. The sibling test
// above leaves CandidateOnly unset and exercises the stored-document branch
// instead, which carries the field either way. directory.err turns any vault
// read into a failed search, so a projection that cannot answer the bound
// cannot hide behind a fallback here.
func TestBleveDiskCandidateSearchAppliesFirstSeenBounds(t *testing.T) {
	documents := firstSeenBoundDocuments()
	directory := newFakeDocumentDirectory(documents...)
	index, err := NewBleveDiskIndex(
		t.Context(), filepath.Join(t.TempDir(), "candidate-first-seen.bleve"),
		directory,
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	directory.loads = 0
	directory.err = errors.New("full document loaded")

	recent, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 1, CandidateOnly: true,
		MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if recent.Total != 1 || len(recent.Results) != 1 ||
		recent.Results[0].URL != "https://a.example/download" {
		t.Fatalf("candidate first-seen bounded results = %#v", recent)
	}
	older, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 1, CandidateOnly: true,
		MaxFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if older.Total != 1 || len(older.Results) != 1 ||
		older.Results[0].URL != "https://a.example/archive" {
		t.Fatalf("candidate first-seen end-bounded results = %#v", older)
	}
	if directory.loads != 0 {
		t.Fatalf("document loads = %d, want the stored projection alone", directory.loads)
	}
}

// TestStoredCandidateProjectionRoundTripsFirstSeen checks the encode/decode pair
// the candidate path depends on, and that the value the projection hands back
// satisfies the filter that reads it.
func TestStoredCandidateProjectionRoundTripsFirstSeen(t *testing.T) {
	firstSeen := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	encoded, err := encodeStoredCandidateProjection(documentstore.Document{
		NormalizedURL: "https://a.example/download",
		ExtractedText: "golang download page",
		FirstSeenAt:   firstSeen,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredCandidateProjection(&search.DocumentMatch{
		Fields: map[string]any{storedCandidateField: encoded},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.FirstSeenAt.Equal(firstSeen) || !decoded.FirstSeenComplete {
		t.Fatalf("decoded projection = %#v", decoded)
	}
	doc := decoded.document("https://a.example/download")
	if !doc.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("projected first-seen time = %v, want %v", doc.FirstSeenAt, firstSeen)
	}
	if !allowsFirstSeen(doc, SearchRequest{MinFirstSeen: firstSeen.Add(-time.Hour)}) {
		t.Fatal("a round-tripped first-seen time must satisfy a window that contains it")
	}
	if allowsFirstSeen(doc, SearchRequest{MinFirstSeen: firstSeen.Add(time.Hour)}) {
		t.Fatal("a round-tripped first-seen time must drop from a later window")
	}
}

// TestStoredCandidateProjectionPredatingFirstSeenReadsStoredDocument covers the
// payloads already on disk. Adding a JSON key does not change the bleve field
// mapping, so no shard is judged stale and no rebuild is triggered: every
// document indexed before this change keeps a payload with no first-seen key.
// An absent key decodes to the same zero time a document with no first-seen time
// carries, so trusting it would refuse the whole index under an active bound.
// Such a payload therefore declines the request and the stored document answers
// it, at the cost of one vault read per hit until the shard is rebuilt.
func TestStoredCandidateProjectionPredatingFirstSeenReadsStoredDocument(t *testing.T) {
	firstSeen := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	doc := documentstore.Document{
		NormalizedURL: "https://a.example/download",
		ExtractedText: "golang download page",
		FirstSeenAt:   firstSeen,
	}
	directory := newFakeDocumentDirectory(doc)
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hit := &search.DocumentMatch{
		ID: doc.NormalizedURL,
		Fields: map[string]any{
			storedCandidateField: `{"s":"golang download page","rc":true}`,
		},
	}
	bounded := SearchRequest{CandidateOnly: true, MinFirstSeen: firstSeen.Add(-time.Hour)}

	projection, found, err := index.loadSearchHitProjection(t.Context(), hit, bounded)
	if err != nil || !found || projection.candidate || directory.loads != 1 {
		t.Fatalf(
			"stale projection=%#v found=%t loads=%d err=%v",
			projection, found, directory.loads, err,
		)
	}
	if !allowsFirstSeen(projection.document, bounded) {
		t.Fatal("the stored document must answer the bound the stale payload cannot")
	}
	// Without a bound nothing reads the field, so the same payload still serves
	// the fast path and buys no vault read.
	directory.loads = 0
	projection, found, err = index.loadSearchHitProjection(
		t.Context(), hit, SearchRequest{CandidateOnly: true},
	)
	if err != nil || !found || !projection.candidate || directory.loads != 0 {
		t.Fatalf(
			"unbounded stale projection=%#v found=%t loads=%d err=%v",
			projection, found, directory.loads, err,
		)
	}
}

// TestStoredCandidateProjectionSupportRequiresFirstSeen states the same rule at
// the predicate that decides it, in both directions and for either half of the
// window.
func TestStoredCandidateProjectionSupportRequiresFirstSeen(t *testing.T) {
	when := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	predating := storedCandidateProjection{RepresentativeComplete: true}
	current := predating
	current.FirstSeenComplete = true

	if !predating.supports(SearchRequest{}) {
		t.Fatal("an unbounded request must accept a payload predating first-seen")
	}
	if predating.supports(SearchRequest{MinFirstSeen: when}) {
		t.Fatal("a start-bounded request must decline a payload predating first-seen")
	}
	if predating.supports(SearchRequest{MaxFirstSeen: when}) {
		t.Fatal("an end-bounded request must decline a payload predating first-seen")
	}
	if !current.supports(SearchRequest{MinFirstSeen: when}) {
		t.Fatal("a start-bounded request must accept a payload carrying first-seen")
	}
	if !current.supports(SearchRequest{MaxFirstSeen: when}) {
		t.Fatal("an end-bounded request must accept a payload carrying first-seen")
	}
}

// firstSeenBoundDocuments returns two pages that carry no publication date at
// all — the download and archive pages that make up the undated third of the
// index — separated only by when this node first saw them.
func firstSeenBoundDocuments() []documentstore.Document {
	return []documentstore.Document{
		{
			NormalizedURL: "https://a.example/download",
			ExtractedText: "golang download page",
			FirstSeenAt:   time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC),
		},
		{
			NormalizedURL: "https://a.example/archive",
			ExtractedText: "golang archive page",
			FirstSeenAt:   time.Date(2025, 1, 5, 8, 0, 0, 0, time.UTC),
		},
	}
}
