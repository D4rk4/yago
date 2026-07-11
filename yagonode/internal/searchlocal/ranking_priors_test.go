package searchlocal

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// TestFreshnessPriorLiftsRecentDocuments pins the SEARCH-38 recency prior
// (Li & Croft-style exponential decay): with equal relevance, a page dated
// this week outranks one dated years ago, while an undated page is neither
// boosted nor punished.
func TestFreshnessPriorLiftsRecentDocuments(t *testing.T) {
	fresh := time.Now().AddDate(0, 0, -3).Format("20060102")
	stale := time.Now().AddDate(-3, 0, 0).Format("20060102")
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 3,
		Results: []searchindex.SearchResult{
			{
				Title: "Stale", URL: "https://a.example/deep/old/page", Score: 1.0,
				PublishedDate: mustDate(t, stale), DateConfidence: 1,
			},
			{
				Title: "Fresh", URL: "https://b.example/deep/new/page", Score: 1.0,
				PublishedDate: mustDate(t, fresh), DateConfidence: 1,
			},
			{Title: "Undated", URL: "https://c.example/deep/nodate/page", Score: 1.0},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Freshness: 0.3}
		},
		nil,
	)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	titles := resultTitles(resp.Results)
	if titles[0] != "Fresh" {
		t.Fatalf("order = %v, want the fresh page first", titles)
	}
	if resp.Results[2].Score >= resp.Results[0].Score {
		t.Fatalf("scores = %+v, freshness must separate the top", resp.Results)
	}
}

// TestFreshnessPriorClampsFutureDates pins the age clamp: a future-dated page is
// treated as brand new (age 0, full freshness bonus) rather than scoring past the
// maximum on a negative age.
func TestFreshnessPriorClampsFutureDates(t *testing.T) {
	future := time.Now().AddDate(1, 0, 0).Format("20060102")
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 1,
		Results: []searchindex.SearchResult{
			{
				Title:          "Future",
				URL:            "https://a.example/p",
				Score:          1.0,
				PublishedDate:  mustDate(t, future),
				DateConfidence: 1,
			},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Freshness: 0.3}
		},
		nil,
	)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Results[0].Score <= 1.0 {
		t.Fatalf("future date must earn the full freshness bonus: %v", resp.Results[0].Score)
	}
}

func TestFreshnessPriorUsesDateConfidence(t *testing.T) {
	published := time.Now().AddDate(0, 0, -1)
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{
				Title: "Uncertain", URL: "https://a.example/page", Score: 1,
				PublishedDate: published, DateConfidence: 0.5,
			},
			{
				Title: "Certain", URL: "https://b.example/page", Score: 1,
				PublishedDate: published, DateConfidence: 1,
			},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Freshness: 1}
		},
		nil,
	)
	resp, err := searcher.Search(t.Context(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := resultTitles(resp.Results); got[0] != "Certain" {
		t.Fatalf("order = %v", got)
	}
}

// TestURLLengthPriorPrefersRootPages pins the entry-page prior (Kraaij,
// Westerveld & Hiemstra, SIGIR 2002): with equal relevance a root URL
// outranks a deep path.
func TestURLLengthPriorPrefersRootPages(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{
				Title: "Deep",
				URL:   "https://a.example/very/long/path/to/some/leaf/page.html",
				Score: 1.0,
			},
			{Title: "Root", URL: "https://b.example/", Score: 1.0},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Freshness: 0.1}
		},
		nil,
	)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if titles := resultTitles(resp.Results); titles[0] != "Root" {
		t.Fatalf("order = %v, want the root URL first", titles)
	}
}

func TestURLLengthPriorSaturates(t *testing.T) {
	root := urlLengthPrior("https://a.example/")
	deep := urlLengthPrior("https://a.example/very/long/path/segments/here/leaf.html")
	if root <= deep || root > urlPriorWeight {
		t.Fatalf("priors: root=%f deep=%f", root, deep)
	}
	if urlLengthPrior("://bad url") != 0 {
		t.Fatal("unparseable URL must carry no prior")
	}
}

// TestQualityPriorLiftsCleanContent pins the RANK-02 content-quality prior: with
// equal relevance a clean, prose-like page (high quality score) outranks a
// keyword-stuffed one (low quality score).
func TestQualityPriorLiftsCleanContent(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 3,
		Results: []searchindex.SearchResult{
			{
				Title: "Spam", URL: "https://a.example/page", Score: 1,
				Quality: -1, QualityKnown: true,
			},
			{Title: "Unknown", URL: "https://b.example/page", Score: 1},
			{
				Title: "Clean", URL: "https://c.example/page", Score: 1,
				Quality: 1, QualityKnown: true,
			},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Quality: 0.5}
		},
		nil,
	)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if titles := resultTitles(resp.Results); titles[0] != "Clean" ||
		titles[1] != "Unknown" || titles[2] != "Spam" {
		t.Fatalf("order = %v", titles)
	}
}

// TestProximityPriorLiftsClusteredTerms pins the RANK-03 SDM unordered-window
// feature: with equal relevance a page where the query words cluster together
// outranks one where they merely both appear.
func TestProximityPriorLiftsClusteredTerms(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{Title: "Scattered", URL: "https://a.example/page", Score: 1.0, Proximity: 0.0},
			{Title: "Clustered", URL: "https://b.example/page", Score: 1.0, Proximity: 1.0},
		},
	}}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Proximity: 0.5}
		},
		nil,
	)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if titles := resultTitles(resp.Results); titles[0] != "Clustered" {
		t.Fatalf("order = %v, want the clustered page first", titles)
	}
}

// TestHostRankDefaultEnabled pins the SEARCH-38 default flip: the default
// ranking profile now folds host authority in, and RANK-02/RANK-03 add the
// quality and proximity priors on by default.
func TestHostRankDefaultEnabled(t *testing.T) {
	weights := searchindex.DefaultRankingWeights()
	if weights.HostRank <= 0 || weights.Freshness <= 0 ||
		weights.Quality <= 0 || weights.Proximity <= 0 {
		t.Fatalf("default priors disabled: %+v", weights)
	}
	if err := weights.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	weights.Freshness = -1
	if err := weights.Validate(); err == nil {
		t.Fatal("negative freshness must fail validation")
	}
	negativeQuality := searchindex.DefaultRankingWeights()
	negativeQuality.Quality = -1
	if err := negativeQuality.Validate(); err == nil {
		t.Fatal("negative quality must fail validation")
	}
	negativeProximity := searchindex.DefaultRankingWeights()
	negativeProximity.Proximity = -1
	if err := negativeProximity.Validate(); err == nil {
		t.Fatal("negative proximity must fail validation")
	}
}

func mustDate(t *testing.T, yyyymmdd string) time.Time {
	t.Helper()
	parsed, err := time.Parse("20060102", yyyymmdd)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}

	return parsed
}

var _ = hostrank.AuthorityTable{}
