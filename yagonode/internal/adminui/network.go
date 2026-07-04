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
