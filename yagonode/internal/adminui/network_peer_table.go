package adminui

import (
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// peersPerPage bounds the peer table so a large roster does not render as one
// unbounded wall; the operator pages through the rest.
const peersPerPage = 50

// peerSortColumns is the allowlist of sortable peer columns. An arbitrary
// `?psort=` value that is not a key here is ignored (the source order stays), so
// the query parameter can never select an unintended field.
var peerSortColumns = map[string]bool{
	"name":     true,
	"type":     true,
	"address":  true,
	"rwi":      true,
	"lastseen": true,
	"health":   true,
	"age":      true,
}

// PeerTableView is the peer roster narrowed to one sorted, paginated page. Every
// header link and the prev/next URLs are built server-side (query-encoded) so
// the template never assembles a URL and the whole table degrades to plain
// links that work without JavaScript.
type PeerTableView struct {
	Peers   []NetworkPeer
	SortKey string
	SortDir string
	Total   int
	Page    int
	Pages   int
	Start   int
	End     int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

// buildPeerTable sorts the roster by the requested column and slices out the
// requested page. An unknown sort key preserves the source order; an
// out-of-range or unparsable page clamps into [1, Pages].
func buildPeerTable(peers []NetworkPeer, rawSort, rawDir, rawPage string) PeerTableView {
	sortKey := rawSort
	if !peerSortColumns[sortKey] {
		sortKey = ""
	}
	dir := rawDir
	if dir != "asc" && dir != "desc" {
		dir = peerSortDefaultDir(sortKey)
	}

	sorted := make([]NetworkPeer, len(peers))
	copy(sorted, peers)
	if sortKey != "" {
		sortPeers(sorted, sortKey, dir)
	}

	total := len(sorted)
	pages := (total + peersPerPage - 1) / peersPerPage
	if pages < 1 {
		pages = 1
	}
	page := 1
	if n, err := strconv.Atoi(rawPage); err == nil && n > 1 {
		page = n
	}
	if page > pages {
		page = pages
	}

	start := (page - 1) * peersPerPage
	end := start + peersPerPage
	if end > total {
		end = total
	}

	view := PeerTableView{
		Peers:   sorted[start:end],
		SortKey: sortKey,
		SortDir: dir,
		Total:   total,
		Page:    page,
		Pages:   pages,
		HasPrev: page > 1,
		HasNext: page < pages,
	}
	if total > 0 {
		view.Start = start + 1
		view.End = end
	}
	if view.HasPrev {
		view.PrevURL = peerTableURL(sortKey, dir, page-1)
	}
	if view.HasNext {
		view.NextURL = peerTableURL(sortKey, dir, page+1)
	}

	return view
}

// peerSortDefaultDir picks the first-click direction for a column: numeric
// columns open descending (the biggest counts and the healthiest peers first),
// text columns ascending.
func peerSortDefaultDir(key string) string {
	switch key {
	case "rwi", "health", "age", "lastseen":
		return "desc"
	default:
		return "asc"
	}
}

func sortPeers(peers []NetworkPeer, key, dir string) {
	less := peerLess(key)
	sort.SliceStable(peers, func(i, j int) bool {
		if dir == "desc" {
			return less(peers[j], peers[i])
		}

		return less(peers[i], peers[j])
	})
}

// peerLess returns the ascending comparison for a column, always breaking ties
// by hash so the order is stable and deterministic across requests.
func peerLess(key string) func(a, b NetworkPeer) bool {
	switch key {
	case "rwi":
		return peerIntLess(func(p NetworkPeer) int { return p.RWICount })
	case "health":
		return peerIntLess(func(p NetworkPeer) int { return p.Health })
	case "age":
		return peerIntLess(func(p NetworkPeer) int { return p.AgeDays })
	case "lastseen":
		return peerTimeLess(func(p NetworkPeer) time.Time { return p.LastSeenAt })
	case "type":
		return peerTextLess(func(p NetworkPeer) string { return p.Type })
	case "address":
		return peerAddressLess
	default: // name
		return peerTextLess(func(p NetworkPeer) string { return p.Name })
	}
}

// peerTimeLess orders peers ascending by a timestamp field (oldest first, so a
// zero "never seen" time sorts oldest), hash breaking ties.
func peerTimeLess(field func(NetworkPeer) time.Time) func(a, b NetworkPeer) bool {
	return func(a, b NetworkPeer) bool {
		if fa, fb := field(a), field(b); !fa.Equal(fb) {
			return fa.Before(fb)
		}

		return a.Hash < b.Hash
	}
}

// peerAddressLess orders peers by network address: an address that parses as an
// IP sorts in numeric IP order and ahead of a hostname/unparsable address,
// which falls back to a case-folded string order. Hash breaks ties.
func peerAddressLess(a, b NetworkPeer) bool {
	ipA, okA := peerIP(a.Address)
	ipB, okB := peerIP(b.Address)
	switch {
	case okA && okB:
		if c := ipA.Compare(ipB); c != 0 {
			return c < 0
		}
	case okA != okB:
		return okA
	default:
		if la, lb := strings.ToLower(a.Address), strings.ToLower(b.Address); la != lb {
			return la < lb
		}
	}

	return a.Hash < b.Hash
}

// peerIP extracts the IP from a "host:port" or bare address, reporting false for
// a hostname or an unparsable value.
func peerIP(address string) (netip.Addr, bool) {
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr, true
}

// peerIntLess orders peers ascending by a numeric field, hash breaking ties.
func peerIntLess(field func(NetworkPeer) int) func(a, b NetworkPeer) bool {
	return func(a, b NetworkPeer) bool {
		if fa, fb := field(a), field(b); fa != fb {
			return fa < fb
		}

		return a.Hash < b.Hash
	}
}

// peerTextLess orders peers ascending by a case-folded text field, hash breaking
// ties.
func peerTextLess(field func(NetworkPeer) string) func(a, b NetworkPeer) bool {
	return func(a, b NetworkPeer) bool {
		if fa, fb := strings.ToLower(field(a)), strings.ToLower(field(b)); fa != fb {
			return fa < fb
		}

		return a.Hash < b.Hash
	}
}

// peerTableURL query-encodes one peer-table view. It anchors to #peers so a
// sort or page click lands the reader back at the table instead of the top of
// the Network page.
func peerTableURL(sortKey, dir string, page int) string {
	values := url.Values{}
	if sortKey != "" {
		values.Set("psort", sortKey)
		values.Set("pdir", dir)
	}
	if page > 1 {
		values.Set("ppage", strconv.Itoa(page))
	}
	encoded := values.Encode()
	if encoded == "" {
		return networkPath + "#peers"
	}

	return networkPath + "?" + encoded + "#peers"
}

// ColumnURL is the sort link for a column header. It activates the column at its
// natural default direction, and flips the direction when the column is already
// active, so a header toggles asc/desc on repeated clicks. Sorting always
// returns to the first page.
func (v PeerTableView) ColumnURL(key string) string {
	dir := peerSortDefaultDir(key)
	if v.SortKey == key && v.SortDir == dir {
		if dir == "asc" {
			dir = "desc"
		} else {
			dir = "asc"
		}
	}

	return peerTableURL(key, dir, 1)
}

// ColumnAriaSort reports the aria-sort value for a column header so assistive
// technology announces the active sort.
func (v PeerTableView) ColumnAriaSort(key string) string {
	if v.SortKey != key {
		return "none"
	}
	if v.SortDir == "desc" {
		return "descending"
	}

	return "ascending"
}

// ColumnIndicator is the ▲/▼ glyph appended to the active column's header, empty
// for inactive columns.
func (v PeerTableView) ColumnIndicator(key string) string {
	if v.SortKey != key {
		return ""
	}
	if v.SortDir == "desc" {
		return "▼"
	}

	return "▲"
}
