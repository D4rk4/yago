package dhtexchange

const (
	DefaultMinimumConnectedPeers = 33
	DefaultMinimumRWIWords       = 100
)

type GateName string

const (
	GateOnlineCaution       GateName = "online_caution"
	GatePublicReachability  GateName = "public_reachability"
	GateLocalPeer           GateName = "local_peer"
	GateLocalPeerMature     GateName = "local_peer_mature"
	GateNetworkSize         GateName = "network_size"
	GateNetworkDHT          GateName = "network_dht"
	GateDistributionEnabled GateName = "distribution_enabled"
	GateLocalRWI            GateName = "local_rwi"
	GateCrawlIdle           GateName = "crawl_idle"
	GateIndexIdle           GateName = "index_idle"
	GateStorageAvailable    GateName = "storage_available"
)

const (
	GateOpenReason                 = "open"
	GateOnlineCautionReason        = "online caution is active"
	GatePublicReachabilityReason   = "public endpoint is not reachable"
	GateLocalPeerMissingReason     = "local peer seed is unavailable"
	GateLocalPeerVirginReason      = "local peer is virgin"
	GateNetworkTooSmallReason      = "network is too small"
	GateNetworkDHTDisabledReason   = "network dht is disabled"
	GateDistributionDisabledReason = "index distribution is disabled"
	GateLocalRWITooSmallReason     = "not enough local rwi words"
	GateCrawlActiveReason          = "crawl is in progress"
	GateIndexActiveReason          = "indexing is in progress"
	GateStorageUnavailableReason   = "storage is unavailable"
)

type GateConfig struct {
	NetworkDHTEnabled    bool
	DistributionEnabled  bool
	AllowWhileCrawling   bool
	AllowWhileIndexing   bool
	MinimumConnectedPeer int
	MinimumRWIWord       int
}

type GateState struct {
	OnlineCaution    string
	PublicReachable  bool
	LocalPeerKnown   bool
	LocalPeerVirgin  bool
	ConnectedPeers   int
	LocalRWIWords    int
	CrawlQueueSize   int
	IndexQueueSize   int
	StorageAvailable bool
}

type GateResult struct {
	Name   GateName
	Open   bool
	Reason string
}

type GateReport struct {
	Open           bool
	BlockingReason string
	Results        []GateResult
}

func DefaultGateConfig() GateConfig {
	return GateConfig{
		NetworkDHTEnabled:    true,
		DistributionEnabled:  true,
		AllowWhileCrawling:   false,
		AllowWhileIndexing:   true,
		MinimumConnectedPeer: DefaultMinimumConnectedPeers,
		MinimumRWIWord:       DefaultMinimumRWIWords,
	}
}

func EvaluateGates(state GateState, config GateConfig) GateReport {
	if config.MinimumConnectedPeer <= 0 {
		config.MinimumConnectedPeer = DefaultMinimumConnectedPeers
	}
	if config.MinimumRWIWord <= 0 {
		config.MinimumRWIWord = DefaultMinimumRWIWords
	}

	results := []GateResult{
		gate(GateOnlineCaution, state.OnlineCaution == "", GateOnlineCautionReason),
		gate(GatePublicReachability, state.PublicReachable, GatePublicReachabilityReason),
		gate(GateLocalPeer, state.LocalPeerKnown, GateLocalPeerMissingReason),
		gate(GateLocalPeerMature, !state.LocalPeerVirgin, GateLocalPeerVirginReason),
		gate(
			GateNetworkSize,
			state.ConnectedPeers >= config.MinimumConnectedPeer,
			GateNetworkTooSmallReason,
		),
		gate(GateNetworkDHT, config.NetworkDHTEnabled, GateNetworkDHTDisabledReason),
		gate(GateDistributionEnabled, config.DistributionEnabled, GateDistributionDisabledReason),
		gate(
			GateLocalRWI,
			state.LocalRWIWords >= config.MinimumRWIWord,
			GateLocalRWITooSmallReason,
		),
		gate(
			GateCrawlIdle,
			config.AllowWhileCrawling || state.CrawlQueueSize == 0,
			GateCrawlActiveReason,
		),
		gate(
			GateIndexIdle,
			config.AllowWhileIndexing || state.IndexQueueSize <= 1,
			GateIndexActiveReason,
		),
		gate(GateStorageAvailable, state.StorageAvailable, GateStorageUnavailableReason),
	}

	report := GateReport{Open: true, Results: results}
	for _, result := range results {
		if result.Open {
			continue
		}
		report.Open = false
		if report.BlockingReason == "" {
			report.BlockingReason = result.Reason
		}
	}

	return report
}

func gate(name GateName, open bool, reason string) GateResult {
	if open {
		reason = GateOpenReason
	}

	return GateResult{Name: name, Open: open, Reason: reason}
}
