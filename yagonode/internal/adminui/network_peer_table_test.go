package adminui

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func peerNames(view PeerTableView) []string {
	names := make([]string, len(view.Peers))
	for i, p := range view.Peers {
		names[i] = p.Name
	}

	return names
}

func TestBuildPeerTableSortsByColumn(t *testing.T) {
	t.Parallel()

	roster := []NetworkPeer{
		{Name: "bravo", Hash: "B", Type: "senior", RWICount: 10, Health: 50, AgeDays: 9},
		{Name: "alpha", Hash: "A", Type: "junior", RWICount: 30, Health: 90, AgeDays: 3},
		{Name: "delta", Hash: "D", Type: "senior", RWICount: 20, Health: 10, AgeDays: 1},
	}
	for _, tc := range []struct {
		sort, dir string
		want      []string
	}{
		{"name", "asc", []string{"alpha", "bravo", "delta"}},
		{"name", "desc", []string{"delta", "bravo", "alpha"}},
		{"rwi", "desc", []string{"alpha", "delta", "bravo"}},
		{"health", "asc", []string{"delta", "bravo", "alpha"}},
		{"age", "desc", []string{"bravo", "alpha", "delta"}},
		// junior < senior, then the senior tie breaks by hash (B < D).
		{"type", "asc", []string{"alpha", "bravo", "delta"}},
	} {
		view := buildPeerTable(roster, tc.sort, tc.dir, "")
		if got := peerNames(view); !slices.Equal(got, tc.want) {
			t.Errorf("sort %s/%s = %v, want %v", tc.sort, tc.dir, got, tc.want)
		}
	}
}

func TestBuildPeerTableUnknownSortPreservesSourceOrder(t *testing.T) {
	t.Parallel()

	roster := []NetworkPeer{
		{Name: "bravo", Hash: "B"},
		{Name: "alpha", Hash: "A"},
	}
	view := buildPeerTable(roster, "bogus", "asc", "")
	if view.SortKey != "" {
		t.Fatalf("SortKey = %q, want empty for an unknown column", view.SortKey)
	}
	if got := peerNames(view); !slices.Equal(got, []string{"bravo", "alpha"}) {
		t.Fatalf("order = %v, want the source order preserved", got)
	}
}

func TestBuildPeerTableSortsByAddressAndLastSeen(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	roster := []NetworkPeer{
		{Name: "c", Hash: "C", Address: "10.0.0.9:8090", LastSeenAt: base.Add(2 * time.Hour)},
		{Name: "a", Hash: "A", Address: "9.0.0.1:8090", LastSeenAt: base},
		{Name: "b", Hash: "B", Address: "example.org:8090", LastSeenAt: base.Add(time.Hour)},
	}

	// IP-aware: 9.0.0.1 sorts before 10.0.0.9 numerically (not lexically), and a
	// hostname address sorts after the numeric IPs.
	byAddr := peerNames(buildPeerTable(roster, "address", "asc", ""))
	if !slices.Equal(byAddr, []string{"a", "c", "b"}) {
		t.Fatalf("address asc = %v, want [a c b] (numeric IPs first, hostname last)", byAddr)
	}

	// Last seen opens most-recent-first.
	if got := (PeerTableView{}).ColumnURL("lastseen"); !strings.Contains(got, "pdir=desc") {
		t.Fatalf("lastseen default link = %q, want descending", got)
	}
	byRecent := peerNames(buildPeerTable(roster, "lastseen", "desc", ""))
	if !slices.Equal(byRecent, []string{"c", "b", "a"}) {
		t.Fatalf("lastseen desc = %v, want [c b a] (newest first)", byRecent)
	}
}

func TestBuildPeerTablePaginates(t *testing.T) {
	t.Parallel()

	roster := make([]NetworkPeer, 270)
	for i := range roster {
		roster[i] = NetworkPeer{Name: fmt.Sprintf("peer%03d", i), Hash: fmt.Sprintf("H%03d", i)}
	}

	first := buildPeerTable(roster, "", "", "")
	if first.Total != 270 || first.Pages != 14 || len(first.Peers) != peersPerPage {
		t.Fatalf("page 1 = total %d pages %d len %d", first.Total, first.Pages, len(first.Peers))
	}
	if first.Page != 1 || first.HasPrev || !first.HasNext || first.Start != 1 || first.End != 20 {
		t.Fatalf("page 1 nav = %+v", first)
	}
	if !strings.Contains(first.NextURL, "ppage=2") || !strings.HasSuffix(first.NextURL, "#peers") {
		t.Fatalf("next url = %q", first.NextURL)
	}

	middle := buildPeerTable(roster, "", "", "2")
	if middle.Page != 2 || !middle.HasPrev || !middle.HasNext || middle.Start != 21 ||
		middle.End != 40 {
		t.Fatalf("page 2 nav = %+v", middle)
	}

	last := buildPeerTable(roster, "", "", "14")
	if last.Page != 14 || !last.HasPrev || last.HasNext || len(last.Peers) != 10 ||
		last.Start != 261 || last.End != 270 {
		t.Fatalf("page 14 = %+v (len %d)", last, len(last.Peers))
	}

	if clamped := buildPeerTable(roster, "", "", "99"); clamped.Page != 14 {
		t.Fatalf("out-of-range page = %d, want clamp to 14", clamped.Page)
	}
	if bad := buildPeerTable(roster, "", "", "not-a-number"); bad.Page != 1 {
		t.Fatalf("unparsable page = %d, want 1", bad.Page)
	}

	sorted := buildPeerTable(roster, "name", "desc", "7")
	for _, target := range []string{sorted.PrevURL, sorted.NextURL} {
		if !strings.Contains(target, "psort=name") || !strings.Contains(target, "pdir=desc") {
			t.Fatalf("sorted page navigation lost sort state: %q", target)
		}
	}
}

func TestBuildPeerTableSinglePageHasNoPager(t *testing.T) {
	t.Parallel()

	view := buildPeerTable([]NetworkPeer{{Name: "only", Hash: "O"}}, "", "", "")
	if view.Pages != 1 || view.HasPrev || view.HasNext {
		t.Fatalf("single page nav = %+v", view)
	}
}

func TestPeerTableColumnURLToggles(t *testing.T) {
	t.Parallel()

	// A numeric column opens descending; a second click on the active column
	// flips it to ascending.
	inactive := PeerTableView{}
	if got := inactive.ColumnURL("rwi"); !strings.Contains(got, "psort=rwi") ||
		!strings.Contains(got, "pdir=desc") {
		t.Fatalf("inactive rwi link = %q, want default descending", got)
	}
	activeDesc := PeerTableView{SortKey: "rwi", SortDir: "desc"}
	if got := activeDesc.ColumnURL("rwi"); !strings.Contains(got, "pdir=asc") {
		t.Fatalf("active-desc rwi link = %q, want a flip to ascending", got)
	}

	// A text column opens ascending and flips to descending when active.
	activeName := PeerTableView{SortKey: "name", SortDir: "asc"}
	if got := activeName.ColumnURL("name"); !strings.Contains(got, "pdir=desc") {
		t.Fatalf("active-asc name link = %q, want a flip to descending", got)
	}
	pageFour := PeerTableView{SortKey: "name", SortDir: "asc", Page: 4}
	if got := pageFour.ColumnURL("name"); strings.Contains(got, "ppage=") {
		t.Fatalf("sort link = %q, want page reset", got)
	}

	if activeDesc.ColumnAriaSort("rwi") != "descending" {
		t.Fatal("active rwi column must report aria-sort descending")
	}
	if activeDesc.ColumnAriaSort("name") != "none" {
		t.Fatal("inactive column must report aria-sort none")
	}
	if activeDesc.ColumnIndicator("rwi") != "▼" || activeName.ColumnIndicator("name") != "▲" {
		t.Fatal("sort indicator glyphs wrong")
	}
	if activeName.ColumnIndicator("rwi") != "" {
		t.Fatal("inactive column must have no indicator")
	}
}

func TestConsoleNetworkPeerTableSortLinksAndPager(t *testing.T) {
	t.Parallel()

	roster := make([]NetworkPeer, 270)
	for i := range roster {
		roster[i] = NetworkPeer{
			Name:      fmt.Sprintf("peer%02d", i),
			Hash:      fmt.Sprintf("H%02d", i),
			RWICount:  i,
			HealthTag: "healthy",
		}
	}
	console := New(
		Options{Network: fakeNetwork{snap: NetworkStatus{Available: true, Peers: roster}}},
	)

	first := do(t, console, "/admin/network")
	if first.status != http.StatusOK {
		t.Fatalf("status %d", first.status)
	}
	for _, want := range []string{
		`href="/admin/network?`, "psort=rwi", "psort=address", "psort=lastseen",
		`aria-sort="none"`, "Next ›", "Page 1 of 14", "peers 1–20 of 270",
	} {
		if !strings.Contains(first.body, want) {
			t.Fatalf("network page missing %q", want)
		}
	}
	if strings.Contains(first.body, ">peer265<") {
		t.Fatal("peer265 belongs on page 14, not the first page")
	}

	last := do(t, console, "/admin/network?ppage=14")
	if !strings.Contains(last.body, ">peer265<") || !strings.Contains(last.body, "‹ Previous") ||
		!strings.Contains(
			last.body,
			"peers 261–270 of 270",
		) || strings.Contains(last.body, "Next ›") {
		t.Fatal("page 14 should show the tail peers, its range, and only a previous link")
	}
}

func TestConsoleNetworkPeerTableSortsDescending(t *testing.T) {
	t.Parallel()

	snap := NetworkStatus{Available: true, Peers: []NetworkPeer{
		{Name: "rwismall", Hash: "L", RWICount: 1, HealthTag: "healthy"},
		{Name: "rwibig", Hash: "H", RWICount: 99, HealthTag: "healthy"},
	}}
	got := do(
		t,
		New(Options{Network: fakeNetwork{snap: snap}}),
		"/admin/network?psort=rwi&pdir=desc",
	)
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `aria-sort="descending"`) {
		t.Fatal("expected the RWI column marked descending")
	}
	big := strings.Index(got.body, "rwibig")
	small := strings.Index(got.body, "rwismall")
	if big < 0 || small < 0 || big > small {
		t.Fatalf("expected rwibig before rwismall under rwi-desc: big=%d small=%d", big, small)
	}
}
