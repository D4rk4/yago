package searchcore

import "testing"

func TestConsolidateClustersSelectsPresentRepresentativeAtBestRank(t *testing.T) {
	bestEvidence := NewRankingEvidence(
		RankingSignalValue{Signal: SignalSourceCount, Value: 1},
		RankingSignalValue{Signal: SignalLocalRank, Value: 1},
	)
	representativeEvidence := NewRankingEvidence(
		RankingSignalValue{Signal: SignalSourceCount, Value: 2},
		RankingSignalValue{Signal: SignalPeerSupport, Value: 3},
		RankingSignalValue{Signal: SignalRemoteRank, Value: 2},
	)
	results := []Result{
		{
			Title:                 "Best member",
			URL:                   "https://a.example",
			ClusterID:             "cluster",
			RepresentativeURL:     "https://b.example",
			Score:                 9,
			Evidence:              bestEvidence,
			diversityRelevance:    0.8,
			diversityRelevanceSet: true,
		},
		{Title: "Independent", URL: "https://independent.example", Score: 8},
		{
			Title:             "Representative",
			URL:               "https://b.example",
			ClusterID:         "cluster",
			RepresentativeURL: "https://b.example",
			Score:             7,
			Evidence:          representativeEvidence,
			Source:            SourceRemote,
		},
	}
	got := ConsolidateClusters(results)
	if len(got) != 2 || got[0].URL != "https://b.example" ||
		got[0].Title != "Representative" || got[0].Score != 9 ||
		got[1].URL != "https://independent.example" {
		t.Fatalf("consolidated = %#v", got)
	}
	if !got[0].diversityRelevanceSet || got[0].diversityRelevance != 0.8 {
		t.Fatalf("diversity relevance = %#v", got[0])
	}
	for signal, want := range map[RankingSignal]float64{
		SignalSourceCount: 3,
		SignalPeerSupport: 3,
		SignalLocalRank:   1,
		SignalRemoteRank:  2,
	} {
		if value, known := got[0].Evidence.Value(signal); !known || value != want {
			t.Fatalf("signal %s = %v, %v, want %v", signal.Name(), value, known, want)
		}
	}
}

func TestConsolidateClustersFallsBackToBestRankedMember(t *testing.T) {
	results := []Result{
		{
			URL:               "https://best.example",
			ClusterID:         "cluster",
			RepresentativeURL: "https://absent.example",
			Score:             4,
		},
		{
			URL:               "https://lower.example",
			ClusterID:         "cluster",
			RepresentativeURL: "https://absent.example",
			Score:             3,
		},
	}
	got := ConsolidateClusters(results)
	if len(got) != 1 || got[0].URL != "https://best.example" ||
		got[0].RepresentativeURL != "https://absent.example" {
		t.Fatalf("fallback = %#v", got)
	}
}

func TestConsolidateClustersKeepsUnknownAndSingleResults(t *testing.T) {
	single := []Result{{URL: "https://single.example"}}
	if got := ConsolidateClusters(single); len(got) != 1 || got[0].URL != single[0].URL {
		t.Fatalf("single = %#v", got)
	}
	results := []Result{
		{URL: "https://a.example"},
		{URL: "https://b.example"},
		{URL: "https://cluster.example", ClusterID: "one"},
	}
	got := ConsolidateClusters(results)
	if len(got) != len(results) {
		t.Fatalf("unknown clusters = %#v", got)
	}
}

func TestConsolidateClustersResolvesConflictingHintsDeterministically(t *testing.T) {
	results := []Result{
		{
			URL:               "https://b.example",
			ClusterID:         "cluster",
			RepresentativeURL: "https://b.example",
		},
		{
			URL:               "https://a.example",
			ClusterID:         "cluster",
			RepresentativeURL: "https://a.example",
		},
	}
	got := ConsolidateClusters(results)
	if len(got) != 1 || got[0].URL != "https://a.example" {
		t.Fatalf("conflicting representative hints = %#v", got)
	}
	withoutHints := []Result{
		{URL: "https://best.example", ClusterID: "cluster", Score: 2},
		{URL: "https://lower.example", ClusterID: "cluster", Score: 1},
	}
	if got := ConsolidateClusters(withoutHints); len(got) != 1 ||
		got[0].URL != "https://best.example" {
		t.Fatalf("missing representative hints = %#v", got)
	}
}

func TestDiversifyResultsConsolidatesStoredClustersFirst(t *testing.T) {
	results := []Result{
		{URL: "https://a.example", ClusterID: "cluster", RepresentativeURL: "https://b.example"},
		{URL: "https://b.example", ClusterID: "cluster", RepresentativeURL: "https://b.example"},
	}
	got := DiversifyResults(results, Request{SiteHost: "example"})
	if len(got) != 1 || got[0].URL != "https://b.example" {
		t.Fatalf("diversified cluster = %#v", got)
	}
}
