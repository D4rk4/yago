package searchcore

import "testing"

func TestFuseByReciprocalRankMergesRanksNotScores(t *testing.T) {
	local := []Result{
		{URL: "https://a.example/1", Title: "A", Score: 990.0},
		{URL: "https://a.example/2", Title: "B", Score: 800.0},
		{URL: "https://a.example/3", Title: "C", Score: 700.0},
	}
	remote := []Result{
		{URL: "https://a.example/3", Title: "C-remote", Score: 0.002},
		{URL: "https://a.example/9", Title: "R", Score: 0.001},
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
}
