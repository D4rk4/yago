package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

func TestBuildDHTOutboundRuntimePrefersExternalReachabilityEvidence(t *testing.T) {
	config := testConfig(t)
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	report := nodestatus.NewReport(nodeIdentity(config), nodestatus.ReportSources{
		RWI:                storage.postings,
		URLs:               storage.urlDirectory,
		Peers:              fakeRoster{},
		News:               fakeSeedNews{},
		Transfers:          fakeTransferTotals{},
		PeerClassification: evidence,
	})
	direct := &publicReachabilityScript{known: true}
	observer := yagomodel.Hash("AAAAAAAAAAAA")
	evidence.Observe(observer, yagomodel.PeerSenior)
	process := buildDHTOutboundRuntime(dhtOutboundRuntimeAssembly{
		ctx:                  t.Context(),
		config:               config,
		storage:              storageVault,
		nodeStorage:          storage,
		report:               report,
		roster:               reachableRoster{},
		reachability:         direct,
		externalReachability: evidence,
	})

	result := process.gateStatus.response(t.Context())
	status := result.State
	if !status.PublicReachable || !status.PublicReachabilityKnown || status.LocalPeerVirgin {
		t.Fatal("peer-confirmed external reachability was not reported")
	}
	if gate, found := gateResponseByName(
		result,
		dhtexchange.GateLocalPeerMature,
	); !found ||
		!gate.Open {
		t.Fatalf("senior local peer maturity gate = %+v found=%t", gate, found)
	}
	if direct.calls.Load() != 0 {
		t.Fatalf("direct probe calls = %d, want 0", direct.calls.Load())
	}

	evidence.Observe(observer, yagomodel.PeerJunior)
	direct.reachable = true
	result = process.gateStatus.response(t.Context())
	status = result.State
	if status.PublicReachable || !status.PublicReachabilityKnown || status.LocalPeerVirgin {
		t.Fatal("direct probe overrode explicit junior peer evidence")
	}
	if gate, found := gateResponseByName(
		result,
		dhtexchange.GateLocalPeerMature,
	); !found ||
		!gate.Open {
		t.Fatalf("junior local peer maturity gate = %+v found=%t", gate, found)
	}
	if direct.calls.Load() != 0 {
		t.Fatalf("direct fallback calls = %d, want 0", direct.calls.Load())
	}
}

func TestBuildDHTOutboundRuntimeFallsBackOnlyWhenExternalEvidenceIsUnknown(t *testing.T) {
	config := testConfig(t)
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	report := nodestatus.NewReport(nodeIdentity(config), nodestatus.ReportSources{
		RWI: storage.postings, URLs: storage.urlDirectory, Peers: fakeRoster{},
		News: fakeSeedNews{}, Transfers: fakeTransferTotals{}, PeerClassification: evidence,
	})
	direct := &publicReachabilityScript{
		reachable: true, known: true, source: publicReachabilitySourceDerivedProbe,
	}
	process := buildDHTOutboundRuntime(dhtOutboundRuntimeAssembly{
		ctx: t.Context(), config: config, storage: storageVault, nodeStorage: storage,
		report: report, roster: reachableRoster{}, reachability: direct,
		externalReachability: evidence,
	})

	result := process.gateStatus.response(t.Context())
	if result.State.PublicReachable ||
		result.State.PublicReachabilityKnown ||
		!result.State.LocalPeerVirgin ||
		result.State.PublicReachabilitySource != adminReachabilitySource(
			publicReachabilitySourceDerivedProbe,
		) ||
		direct.calls.Load() != 1 {
		t.Fatalf("derived local fallback = %+v, calls = %d", result, direct.calls.Load())
	}
	if gate, found := gateResponseByName(
		result,
		dhtexchange.GateLocalPeerMature,
	); !found ||
		gate.Open ||
		gate.Reason != dhtexchange.GateLocalPeerVirginReason {
		t.Fatalf("virgin local peer maturity gate = %+v found=%t", gate, found)
	}
	direct.source = publicReachabilitySourcePinnedProbe
	result = process.gateStatus.response(t.Context())
	if !result.State.PublicReachable ||
		!result.State.PublicReachabilityKnown ||
		result.State.PublicReachabilitySource != adminReachabilitySource(
			publicReachabilitySourcePinnedProbe,
		) ||
		direct.calls.Load() != 2 {
		t.Fatalf("pinned public fallback = %+v, calls = %d", result, direct.calls.Load())
	}
}

func gateResponseByName(
	response dhtGateStatusResponse,
	name dhtexchange.GateName,
) (dhtGateResultResponse, bool) {
	for _, gate := range response.Gates {
		if gate.Name == string(name) {
			return gate, true
		}
	}

	return dhtGateResultResponse{}, false
}
