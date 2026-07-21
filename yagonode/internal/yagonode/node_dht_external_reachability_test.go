package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
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
	report := nodestatus.NewReport(nodeIdentity(config), nodestatus.ReportSources{
		RWI:       storage.postings,
		URLs:      storage.urlDirectory,
		Peers:     fakeRoster{},
		News:      fakeSeedNews{},
		Transfers: fakeTransferTotals{},
	})
	direct := &publicReachabilityScript{known: true}
	evidence := peerannouncement.NewExternalReachabilityEvidence()
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

	if !process.gateStatus.response(t.Context()).State.PublicReachable {
		t.Fatal("peer-confirmed external reachability did not open the public gate")
	}
	if direct.calls.Load() != 0 {
		t.Fatalf("direct probe calls = %d, want 0", direct.calls.Load())
	}

	evidence.Observe(observer, yagomodel.PeerJunior)
	direct.reachable = true
	if process.gateStatus.response(t.Context()).State.PublicReachable {
		t.Fatal("direct probe overrode explicit junior peer evidence")
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
	report := nodestatus.NewReport(nodeIdentity(config), nodestatus.ReportSources{
		RWI: storage.postings, URLs: storage.urlDirectory, Peers: fakeRoster{},
		News: fakeSeedNews{}, Transfers: fakeTransferTotals{},
	})
	direct := &publicReachabilityScript{
		reachable: true, known: true, source: publicReachabilitySourceDerivedProbe,
	}
	process := buildDHTOutboundRuntime(dhtOutboundRuntimeAssembly{
		ctx: t.Context(), config: config, storage: storageVault, nodeStorage: storage,
		report: report, roster: reachableRoster{}, reachability: direct,
		externalReachability: peerannouncement.NewExternalReachabilityEvidence(),
	})

	result := process.gateStatus.response(t.Context())
	if result.State.PublicReachable ||
		result.reachability.state != publicReachabilityUnknown ||
		result.reachability.source != publicReachabilitySourceDerivedProbe ||
		direct.calls.Load() != 1 {
		t.Fatalf("derived local fallback = %+v, calls = %d", result, direct.calls.Load())
	}
	direct.source = publicReachabilitySourcePinnedProbe
	result = process.gateStatus.response(t.Context())
	if !result.State.PublicReachable ||
		result.reachability.source != publicReachabilitySourcePinnedProbe ||
		direct.calls.Load() != 2 {
		t.Fatalf("pinned public fallback = %+v, calls = %d", result, direct.calls.Load())
	}
}
