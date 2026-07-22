package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/seedlist"
	"github.com/D4rk4/yago/yagoproto"
)

type publishedPeerTypeCounts struct{}

func (publishedPeerTypeCounts) RWICount(context.Context) (int, error) { return 0, nil }

func (publishedPeerTypeCounts) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return 0, nil
}

func (publishedPeerTypeCounts) Count(context.Context) (int, error) { return 0, nil }

type publishedPeerTypePeers struct{}

func (publishedPeerTypePeers) ReachablePeerCount(context.Context) int { return 0 }

type publishedPeerTypeNews struct{}

func (publishedPeerTypeNews) SeedNews(context.Context) string { return "" }

type publishedPeerTypeTransfers struct{}

func (publishedPeerTypeTransfers) TransferTotals(context.Context) nodestatus.TransferTotals {
	return nodestatus.TransferTotals{}
}

type publishedPeerTypeDirectory struct{}

func (publishedPeerTypeDirectory) ReachablePeers(context.Context) []yagomodel.Seed { return nil }

func (publishedPeerTypeDirectory) SeedlistPeers(context.Context, int) []yagomodel.Seed {
	return nil
}

func (publishedPeerTypeDirectory) PeerByHash(
	context.Context,
	yagomodel.Hash,
) (yagomodel.Seed, bool) {
	return yagomodel.Seed{}, false
}

func (publishedPeerTypeDirectory) PeerByName(
	context.Context,
	string,
) (yagomodel.Seed, bool) {
	return yagomodel.Seed{}, false
}

func TestSeedlistPublishesRuntimeSelfPeerType(t *testing.T) {
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	counts := publishedPeerTypeCounts{}
	report := nodestatus.NewReport(
		nodeidentity.Identity{
			Hash: yagomodel.WordHash("self"),
			Name: "self-peer",
			Host: "192.0.2.10",
			Port: 8090,
		},
		nodestatus.ReportSources{
			RWI: counts, URLs: counts, Peers: publishedPeerTypePeers{},
			News: publishedPeerTypeNews{}, Transfers: publishedPeerTypeTransfers{},
			PeerClassification: evidence,
		},
	)
	mux := http.NewServeMux()
	seedlist.Mount(
		httpguard.NewWireRouter(
			mux,
			httpguard.WireGate{
				Guard: httpguard.NewRequestGuard(
					httpguard.DefaultMaxBodyBytes,
					time.Second,
				),
				Address: httpguard.NewClientAddressResolver(nil),
			},
		),
		report,
		publishedPeerTypeDirectory{},
	)

	assertPublishedPeerType(t, mux, yagomodel.PeerVirgin)
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerSenior)
	assertPublishedPeerType(t, mux, yagomodel.PeerSenior)
}

func assertPublishedPeerType(t *testing.T, endpoint http.Handler, want yagomodel.PeerType) {
	t.Helper()
	query := yagoproto.SeedlistRequest{IncludeSelf: true, OwnSeedOnly: true}.Form().Encode()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSeedlist+"?"+query,
		nil,
	)
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	wireSeeds := strings.Split(strings.TrimSuffix(recorder.Body.String(), "\r\n"), "\r\n")
	if len(wireSeeds) != 1 {
		t.Fatalf("self seed count = %d, want 1", len(wireSeeds))
	}
	published, err := yagomodel.ParseSeedWireForm(t.Context(), wireSeeds[0])
	if err != nil {
		t.Fatalf("parse published self seed: %v", err)
	}
	got, known := published.PeerType.Get()
	if !known || got != want {
		t.Fatalf("self peer type = %q known=%t, want %q true", got, known, want)
	}
}
