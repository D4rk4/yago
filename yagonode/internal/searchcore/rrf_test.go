package searchcore

import "testing"

func TestFuseByReciprocalRankMergesRanksNotScores(t *testing.T) {
	local := []Result{
		{URL: "https://a.example/1", Title: "A", Score: 990.0, Source: SourceLocal},
		{URL: "https://a.example/2", Title: "B", Score: 800.0, Source: SourceLocal},
		{URL: "https://a.example/3", Title: "C", Score: 700.0, Source: SourceLocal},
	}
	remote := []Result{
		{URL: "https://a.example/3", Title: "C-remote", Score: 0.002, Source: SourceRemote},
		{URL: "https://a.example/9", Title: "R", Score: 0.001, Source: SourceRemote},
	}

	fused := FuseByReciprocalRank(local, remote)
	if len(fused) != 4 {
		t.Fatalf("fused = %d results", len(fused))
	}
	// C appears in both lists (rank 3 local + rank 1 remote) and must outrank
	// everything found by a single source.
	if fused[0].URL != "https://a.example/3" {
		t.Fatalf("top fused = %+v", fused[0])
	}
	// Display fields come from the first list carrying the result.
	if fused[0].Title != "C" {
		t.Fatalf("title = %q", fused[0].Title)
	}
	// A (local rank 1) beats R (remote rank 2).
	if fused[1].URL != "https://a.example/1" || fused[3].URL != "https://a.example/9" {
		t.Fatalf("order = %v %v", fused[1].URL, fused[3].URL)
	}
	// Fused weights are RRF sums, not raw scores.
	wantTop := 1.0/float64(rrfK+3) + 1.0/float64(rrfK+1)
	if diff := fused[0].Score - wantTop; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("top weight = %v want %v", fused[0].Score, wantTop)
	}
	for signal, want := range map[RankingSignal]float64{
		SignalLocalRank: 3, SignalRemoteRank: 1, SignalSourceCount: 2, SignalPeerSupport: 1,
	} {
		if got, known := fused[0].Evidence.Value(signal); !known || got != want {
			t.Fatalf("evidence %s = %v/%v, want %v", signal.Name(), got, known, want)
		}
	}
}

func TestFuseByReciprocalRankIdentityAndEdges(t *testing.T) {
	// URL hashes identify duplicates across differing URLs.
	byHash := FuseByReciprocalRank(
		[]Result{{URLHash: "h1", URL: "https://a.example/x"}},
		[]Result{{URLHash: "h1", URL: "https://mirror.example/x"}},
	)
	if len(byHash) != 1 {
		t.Fatalf("hash identity fused = %d", len(byHash))
	}

	if got := FuseByReciprocalRank(); len(got) != 0 {
		t.Fatalf("no lists = %v", got)
	}
	if got := FuseByReciprocalRank(nil, nil); len(got) != 0 {
		t.Fatalf("empty lists = %v", got)
	}
	single := FuseByReciprocalRank([]Result{{URL: "https://only.example"}})
	if len(single) != 1 || single[0].URL != "https://only.example" {
		t.Fatalf("single list = %v", single)
	}
	deduped := FuseByReciprocalRank([]Result{
		{URL: "https://only.example"},
		{URL: "https://only.example"},
	})
	if len(deduped) != 1 || deduped[0].Score != 1.0/float64(rrfK+1) {
		t.Fatalf("intra-list duplicate = %#v", deduped)
	}
	peerMatch := FuseByReciprocalRank(
		[]Result{{URL: "https://shared.example", Source: SourceRemote}},
		[]Result{{URL: "https://shared.example", Source: SourceRemote}},
	)
	if support, known := peerMatch[0].Evidence.Value(SignalPeerSupport); !known || support != 2 {
		t.Fatalf("peer support = %v/%v, want 2", support, known)
	}
}
