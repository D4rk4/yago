package clickcapture

import (
	"context"
	"math"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

func TestPositionWeightClampsAndRises(t *testing.T) {
	// Rank below 1 is clamped to rank 1, and the weight rises with rank so a
	// click on a lower-ranked (less-examined) result carries more signal.
	if got := positionWeight(0); got != 1 {
		t.Errorf("positionWeight(0) = %v, want 1 (clamped to rank 1)", got)
	}
	if got := positionWeight(1); got != 1 {
		t.Errorf("positionWeight(1) = %v, want 1", got)
	}
	if got := positionWeight(3); got != 2 {
		t.Errorf("positionWeight(3) = %v, want 2 (log2 4)", got)
	}
	if positionWeight(10) <= positionWeight(2) {
		t.Errorf("positionWeight must rise with rank: w(10)=%v w(2)=%v",
			positionWeight(10), positionWeight(2))
	}
}

func TestRecordNormalizesAccumulatesAndSorts(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()

	// The same query in different casing/spacing accumulates on one record.
	if err := store.Record(ctx, "  Linux  Kernel ", "https://a.example/", 1); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := store.Record(ctx, "linux kernel", "https://a.example/", 3); err != nil {
		t.Fatalf("record a again: %v", err)
	}
	if err := store.Record(ctx, "linux kernel", "https://b.example/", 2); err != nil {
		t.Fatalf("record b: %v", err)
	}
	if err := store.Record(ctx, "debian", "https://d.example/", 1); err != nil {
		t.Fatalf("record debian: %v", err)
	}

	aggregates, err := store.Aggregates(ctx)
	if err != nil {
		t.Fatalf("aggregates: %v", err)
	}
	if len(aggregates) != 2 {
		t.Fatalf("got %d queries, want 2", len(aggregates))
	}
	// Sorted by query: "debian" before "linux kernel".
	if aggregates[0].Query != "debian" || aggregates[1].Query != "linux kernel" {
		t.Fatalf("unsorted queries: %q, %q", aggregates[0].Query, aggregates[1].Query)
	}
	a := aggregates[1].URLs["https://a.example/"]
	if a.Clicks != 2 {
		t.Errorf("url a clicks = %d, want 2", a.Clicks)
	}
	// Two clicks: rank 1 (weight 1) + rank 3 (weight 2) = 3.
	if a.Weight != 3 {
		t.Errorf("url a weight = %v, want 3", a.Weight)
	}
}

func TestRecordRejectsEmptyQueryAndURL(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.Record(ctx, "   ", "https://a.example/", 1); err == nil {
		t.Error("expected an error for an empty query")
	}
	if err := store.Record(ctx, "query", "   ", 1); err == nil {
		t.Error("expected an error for an empty url")
	}
}

func TestRecordEvictsLightestURLWhenFull(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()

	// A heavily-weighted deep click, then fill the record to capacity with
	// light rank-1 clicks; a further new URL must evict a light one, not the
	// heavy signal.
	if err := store.Record(ctx, "q", "https://heavy.example/", 500); err != nil {
		t.Fatalf("record heavy: %v", err)
	}
	for i := range maxURLsPerQuery - 1 {
		url := "https://light.example/" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if err := store.Record(ctx, "q", url, 1); err != nil {
			t.Fatalf("record light %d: %v", i, err)
		}
	}
	if err := store.Record(ctx, "q", "https://newcomer.example/", 1); err != nil {
		t.Fatalf("record newcomer: %v", err)
	}

	aggregates, err := store.Aggregates(ctx)
	if err != nil {
		t.Fatalf("aggregates: %v", err)
	}
	urls := aggregates[0].URLs
	if len(urls) != maxURLsPerQuery {
		t.Fatalf("record holds %d urls, want capped at %d", len(urls), maxURLsPerQuery)
	}
	if _, ok := urls["https://heavy.example/"]; !ok {
		t.Error("heavy-signal url was evicted, want it kept")
	}
	if _, ok := urls["https://newcomer.example/"]; !ok {
		t.Error("newcomer url was not admitted")
	}
}

func TestAggregatesEmptyStore(t *testing.T) {
	store := openStore(t)
	aggregates, err := store.Aggregates(t.Context())
	if err != nil {
		t.Fatalf("aggregates: %v", err)
	}
	if len(aggregates) != 0 {
		t.Errorf("got %d aggregates from an empty store, want 0", len(aggregates))
	}
}

func TestImplicitJudgmentsFromStore(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	// One query with three clicks on the winner, one on a runner-up.
	for range 3 {
		if err := store.Record(ctx, "go generics", "https://win.example/", 1); err != nil {
			t.Fatalf("record win: %v", err)
		}
	}
	if err := store.Record(ctx, "go generics", "https://also.example/", 9); err != nil {
		t.Fatalf("record also: %v", err)
	}

	judgments, err := store.ImplicitJudgments(ctx, 3)
	if err != nil {
		t.Fatalf("implicit judgments: %v", err)
	}
	if len(judgments) != 1 {
		t.Fatalf("got %d judgments, want 1", len(judgments))
	}
	if judgments[0].Query != "go generics" {
		t.Errorf("query = %q, want %q", judgments[0].Query, "go generics")
	}
	if judgments[0].Relevant["https://win.example/"] != gradeHighlyRelevant {
		t.Errorf("winner grade = %d, want %d",
			judgments[0].Relevant["https://win.example/"], gradeHighlyRelevant)
	}
}

func TestDeriveJudgmentsGradingAndFloors(t *testing.T) {
	aggregates := []QueryClicks{
		{
			Query: "alpha",
			URLs: map[string]URLClicks{
				"https://top.example/":  {Clicks: 4, Weight: 4},
				"https://mid.example/":  {Clicks: 2, Weight: 2.1},
				"https://weak.example/": {Clicks: 1, Weight: 0.5},
			},
		},
		// Below the click floor of 3: dropped as noise.
		{
			Query: "beta",
			URLs:  map[string]URLClicks{"https://one.example/": {Clicks: 1, Weight: 1}},
		},
	}

	judgments := DeriveJudgments(aggregates, 3)
	if len(judgments) != 1 {
		t.Fatalf("got %d judgments, want 1 (beta below floor)", len(judgments))
	}
	relevant := judgments[0].Relevant
	// top weight 4; dominanceFraction 0.5 -> threshold 2.0.
	if relevant["https://top.example/"] != gradeHighlyRelevant {
		t.Errorf("top grade = %d, want %d", relevant["https://top.example/"], gradeHighlyRelevant)
	}
	if relevant["https://mid.example/"] != gradeHighlyRelevant {
		t.Errorf("mid (2.1 >= 2.0) grade = %d, want %d",
			relevant["https://mid.example/"], gradeHighlyRelevant)
	}
	if relevant["https://weak.example/"] != gradeRelevant {
		t.Errorf("weak (0.5 < 2.0) grade = %d, want %d",
			relevant["https://weak.example/"], gradeRelevant)
	}
}

func TestDeriveJudgmentsClampsMinClicksAndSkipsUnclickedURLs(t *testing.T) {
	// minClicks below 1 is clamped to 1; a URL with a zero click count but a
	// stray weight is not graded.
	aggregates := []QueryClicks{
		{
			Query: "gamma",
			URLs: map[string]URLClicks{
				"https://real.example/":  {Clicks: 1, Weight: 1},
				"https://ghost.example/": {Clicks: 0, Weight: 5},
			},
		},
	}
	judgments := DeriveJudgments(aggregates, 0)
	if len(judgments) != 1 {
		t.Fatalf("got %d judgments, want 1", len(judgments))
	}
	if _, graded := judgments[0].Relevant["https://ghost.example/"]; graded {
		t.Error("a URL with zero clicks must not be graded")
	}
	if judgments[0].Relevant["https://real.example/"] != gradeHighlyRelevant {
		t.Errorf("real grade = %d, want %d",
			judgments[0].Relevant["https://real.example/"], gradeHighlyRelevant)
	}
}

func TestDeriveJudgmentsSkipsWhenNoPositiveWeight(t *testing.T) {
	// Total clicks meet the floor but every weight is zero (no examinable
	// signal), so the query yields no judgment.
	aggregates := []QueryClicks{
		{
			Query: "delta",
			URLs:  map[string]URLClicks{"https://flat.example/": {Clicks: 3, Weight: 0}},
		},
	}
	if judgments := DeriveJudgments(aggregates, 1); len(judgments) != 0 {
		t.Fatalf("got %d judgments, want 0 (no positive weight)", len(judgments))
	}
}

func TestDeriveJudgmentsSortsByQuery(t *testing.T) {
	aggregates := []QueryClicks{
		{Query: "zzz", URLs: map[string]URLClicks{"https://z.example/": {Clicks: 2, Weight: 2}}},
		{Query: "aaa", URLs: map[string]URLClicks{"https://a.example/": {Clicks: 2, Weight: 2}}},
	}
	judgments := DeriveJudgments(aggregates, 1)
	if len(judgments) != 2 || judgments[0].Query != "aaa" || judgments[1].Query != "zzz" {
		t.Fatalf("judgments not sorted by query: %+v", judgments)
	}
}

func TestClickCodecRoundTripAndDecodeError(t *testing.T) {
	original := QueryClicks{
		Query: "q",
		URLs:  map[string]URLClicks{"https://a.example/": {Clicks: 2, Weight: 1.5}},
	}
	raw, err := clickCodec{}.Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := clickCodec{}.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Query != "q" || decoded.URLs["https://a.example/"].Clicks != 2 {
		t.Errorf("round trip mismatch: %+v", decoded)
	}
	_, decodeErr := clickCodec{}.Decode([]byte("{not json"))
	if decodeErr == nil {
		t.Error("expected a decode error for malformed JSON")
	}
}

func TestOpenRejectsDuplicateBucket(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := Open(v); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Error("expected an error registering the click bucket twice")
	}
}

func TestStorePropagatesVaultErrors(t *testing.T) {
	store := openStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.Record(ctx, "q", "https://a.example/", 1); err == nil {
		t.Error("Record: expected a vault error on a cancelled context")
	}
	if _, err := store.Aggregates(ctx); err == nil {
		t.Error("Aggregates: expected a vault error on a cancelled context")
	}
	if _, err := store.ImplicitJudgments(ctx, 1); err == nil {
		t.Error("ImplicitJudgments: expected a vault error on a cancelled context")
	}
}

func TestEvictLightestPicksMinimumWeight(t *testing.T) {
	urls := map[string]URLClicks{
		"https://a.example/": {Clicks: 1, Weight: 3},
		"https://b.example/": {Clicks: 1, Weight: 1},
		"https://c.example/": {Clicks: 1, Weight: 2},
	}
	evictLightest(urls)
	if _, ok := urls["https://b.example/"]; ok {
		t.Error("evictLightest kept the minimum-weight url")
	}
	if len(urls) != 2 {
		t.Errorf("after eviction len = %d, want 2", len(urls))
	}
	if math.IsInf(urls["https://a.example/"].Weight, 0) {
		t.Error("unexpected infinite weight")
	}
}
