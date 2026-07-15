package yagonode

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

const pathDHTGates = "/api/admin/v1/network/dht/gates"

type dhtGateStatusSource struct {
	snapshot func(context.Context) dhtexchange.GateState
	config   dhtexchange.GateConfig
}

type dhtGateStatusEndpoint struct {
	source dhtGateStatusSource
}

type dhtGateStatusResponse struct {
	Open           bool                    `json:"open"`
	BlockingReason string                  `json:"blockingReason,omitempty"`
	State          dhtGateStateResponse    `json:"state"`
	Config         dhtGateConfigResponse   `json:"config"`
	Gates          []dhtGateResultResponse `json:"gates"`
}

type dhtGateStateResponse struct {
	OnlineCaution    string `json:"onlineCaution,omitempty"`
	PublicReachable  bool   `json:"publicReachable"`
	LocalPeerKnown   bool   `json:"localPeerKnown"`
	LocalPeerVirgin  bool   `json:"localPeerVirgin"`
	ConnectedPeers   int    `json:"connectedPeers"`
	LocalRWIWords    int    `json:"localRWIWords"`
	LocalRWIKnown    bool   `json:"localRWIKnown"`
	CrawlQueueSize   int    `json:"crawlQueueSize"`
	CrawlQueueKnown  bool   `json:"crawlQueueKnown"`
	IndexQueueSize   int    `json:"indexQueueSize"`
	IndexQueueKnown  bool   `json:"indexQueueKnown"`
	StorageAvailable bool   `json:"storageAvailable"`
	StorageKnown     bool   `json:"storageKnown"`
}

type dhtGateConfigResponse struct {
	NetworkDHTEnabled    bool `json:"networkDHTEnabled"`
	DistributionEnabled  bool `json:"distributionEnabled"`
	AllowWhileCrawling   bool `json:"allowWhileCrawling"`
	AllowWhileIndexing   bool `json:"allowWhileIndexing"`
	MinimumConnectedPeer int  `json:"minimumConnectedPeer"`
	MinimumRWIWord       int  `json:"minimumRWIWord"`
}

type dhtGateResultResponse struct {
	Name   string `json:"name"`
	Open   bool   `json:"open"`
	Reason string `json:"reason"`
}

func newDHTGateStatusEndpoint(source dhtGateStatusSource) http.Handler {
	return dhtGateStatusEndpoint{source: source}
}

func (e dhtGateStatusEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(e.source.response(r.Context()))
}

func (s dhtGateStatusSource) response(ctx context.Context) dhtGateStatusResponse {
	var state dhtexchange.GateState
	if s.snapshot != nil {
		state = s.snapshot(ctx)
	}
	report := dhtexchange.EvaluateGates(state, s.config)

	return dhtGateStatusResponse{
		Open:           report.Open,
		BlockingReason: report.BlockingReason,
		State:          dhtGateState(state),
		Config:         dhtGateConfig(s.config),
		Gates:          dhtGateResults(report.Results),
	}
}

func dhtGateState(state dhtexchange.GateState) dhtGateStateResponse {
	return dhtGateStateResponse{
		OnlineCaution:    state.OnlineCaution,
		PublicReachable:  state.PublicReachable,
		LocalPeerKnown:   state.LocalPeerKnown,
		LocalPeerVirgin:  state.LocalPeerVirgin,
		ConnectedPeers:   state.ConnectedPeers,
		LocalRWIWords:    state.LocalRWIWords,
		LocalRWIKnown:    state.LocalRWIKnown,
		CrawlQueueSize:   state.CrawlQueueSize,
		CrawlQueueKnown:  state.CrawlQueueKnown,
		IndexQueueSize:   state.IndexQueueSize,
		IndexQueueKnown:  state.IndexQueueKnown,
		StorageAvailable: state.StorageAvailable,
		StorageKnown:     state.StorageKnown,
	}
}

func dhtGateConfig(config dhtexchange.GateConfig) dhtGateConfigResponse {
	return dhtGateConfigResponse{
		NetworkDHTEnabled:    config.NetworkDHTEnabled,
		DistributionEnabled:  config.DistributionEnabled,
		AllowWhileCrawling:   config.AllowWhileCrawling,
		AllowWhileIndexing:   config.AllowWhileIndexing,
		MinimumConnectedPeer: config.MinimumConnectedPeer,
		MinimumRWIWord:       config.MinimumRWIWord,
	}
}

func dhtGateResults(results []dhtexchange.GateResult) []dhtGateResultResponse {
	out := make([]dhtGateResultResponse, 0, len(results))
	for _, result := range results {
		out = append(out, dhtGateResultResponse{
			Name:   string(result.Name),
			Open:   result.Open,
			Reason: result.Reason,
		})
	}

	return out
}
