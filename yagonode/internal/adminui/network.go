package adminui

import "context"

// NetworkGate is one DHT distribution gate and whether it is open.
type NetworkGate struct {
	Name   string
	Open   bool
	Reason string
}

// NetworkPeer is one peer summarized for the console peer table.
type NetworkPeer struct {
	Name     string
	Hash     string
	Address  string
	Type     string
	Flags    []string
	RWICount int
	LastSeen string
	AgeDays  int
	Blocked  bool
	// Health is the passive-observation availability score (OPS-12) and
	// HealthTag its healthy/aging/stale band.
	Health    int
	HealthTag string
}

// SeedlistEntry is one configured bootstrap seed-list URL with the outcome of its
// most recent import, shown in the Network section's seedlist table.
type SeedlistEntry struct {
	URL        string
	LastImport string
	Result     string
	OK         bool
	Imported   bool
}

// NetworkFlag is one seed capability flag with the state this node advertises to
// the swarm, so the operator sees every bit peers receive — set or not.
type NetworkFlag struct {
	Name string
	Set  bool
}

// NetworkStatus is the peer-network snapshot the Network section renders.
type NetworkStatus struct {
	Available       bool
	DHTOpen         bool
	PublicReachable bool
	BlockingReason  string
	OwnFlags        []NetworkFlag
	Gates           []NetworkGate
	KnownPeers      int
	ReachablePeers  int
	Peers           []NetworkPeer
	Seedlists       []SeedlistEntry
}

// NetworkSource supplies the network snapshot on each request.
type NetworkSource interface {
	Network(ctx context.Context) NetworkStatus
}

// PeerDetail is one peer's published profile as shown on its detail page: its DNA
// identity, its self-reported statistics, its capability flags, and its per-peer
// index-transfer counters. It is read-only and carries no secrets.
type PeerDetail struct {
	Name          string
	Hash          string
	Address       string
	Version       string
	Type          string
	Flags         []string
	LastSeen      string
	AgeDays       int
	UptimeMinutes int
	RWIWords      int
	URLs          int
	KnownSeeds    int
	SentWords     int64
	ReceivedWords int64
	SentURLs      int64
	ReceivedURLs  int64
	Blocked       bool
}

// PeerDetailSource resolves a peer hash to its detail. It reports false when the
// roster has never seen the hash, so the console can render a 404. A nil provider
// leaves the peer table without drill-down links.
type PeerDetailSource interface {
	PeerDetail(ctx context.Context, hash string) (PeerDetail, bool)
}

// PeerNewsItem is one received peer-news record summarized for the Network
// section: its category, originating peer, humanized age, and payload detail.
type PeerNewsItem struct {
	Category   string
	Originator string
	Age        string
	Detail     string
}

// PeerNewsSource supplies the most recent received peer-news items, newest first.
// A nil provider hides the peer-news sub-view.
type PeerNewsSource interface {
	PeerNews(ctx context.Context) []PeerNewsItem
}

// SeedlistRefreshSource re-imports a configured seed list on the operator's
// behalf, recording the outcome. It rejects a URL that is not configured. A nil
// provider leaves the seedlist table read-only (no refresh action).
type SeedlistRefreshSource interface {
	RefreshSeedlist(ctx context.Context, url string) error
}

// PeerBlockSource blocks or unblocks a peer by hash. It validates the hash and
// refuses to block this node itself. A nil provider hides the block controls.
type PeerBlockSource interface {
	Block(ctx context.Context, hash string) error
	Unblock(ctx context.Context, hash string) error
}
