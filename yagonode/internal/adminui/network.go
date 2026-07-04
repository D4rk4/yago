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
}

// NetworkStatus is the peer-network snapshot the Network section renders.
type NetworkStatus struct {
	Available       bool
	DHTOpen         bool
	PublicReachable bool
	BlockingReason  string
	Gates           []NetworkGate
	KnownPeers      int
	ReachablePeers  int
	Peers           []NetworkPeer
	SeedlistURLs    []string
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
}

// PeerDetailSource resolves a peer hash to its detail. It reports false when the
// roster has never seen the hash, so the console can render a 404. A nil provider
// leaves the peer table without drill-down links.
type PeerDetailSource interface {
	PeerDetail(ctx context.Context, hash string) (PeerDetail, bool)
}
