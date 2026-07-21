package yagonode

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagoproto"
)

func TestExternalFirstPublicReachabilityPreservesEvidencePrecedence(t *testing.T) {
	direct := &publicReachabilityScript{
		reachable: true, known: true, source: publicReachabilitySourcePinnedProbe,
	}
	external := &publicReachabilityScript{known: true, source: publicReachabilitySourcePeerBackPing}
	combined := externalFirstPublicReachability{external: external, direct: direct}

	snapshot := combined.Snapshot(t.Context())
	if snapshot.state != publicReachabilityUnreachable || direct.calls.Load() != 0 {
		t.Fatalf(
			"explicit unreachable snapshot = %+v, direct calls = %d",
			snapshot,
			direct.calls.Load(),
		)
	}
	external.known = false
	snapshot = combined.Snapshot(t.Context())
	if snapshot.state != publicReachabilityReachable || direct.calls.Load() != 1 {
		t.Fatalf(
			"unknown fallback snapshot = %+v, direct calls = %d",
			snapshot,
			direct.calls.Load(),
		)
	}
	withoutFallback := externalFirstPublicReachability{external: external}.Snapshot(t.Context())
	if withoutFallback.state != publicReachabilityUnknown {
		t.Fatalf("missing direct fallback snapshot = %+v", withoutFallback)
	}
	direct.source = publicReachabilitySourceDerivedProbe
	derived := combined.Snapshot(t.Context())
	if derived.state != publicReachabilityUnknown ||
		derived.source != publicReachabilitySourceDerivedProbe {
		t.Fatalf("derived local fallback snapshot = %+v", derived)
	}
}

func TestPeerBackPingPublicReachabilityMapsEvidenceStateAndTime(t *testing.T) {
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	adapter := peerBackPingPublicReachability{source: evidence}
	unknown := adapter.Snapshot(t.Context())
	if unknown.state != publicReachabilityUnknown ||
		unknown.source != publicReachabilitySourcePeerBackPing {
		t.Fatalf("unknown external snapshot = %+v", unknown)
	}
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerJunior)
	unreachable := adapter.Snapshot(t.Context())
	if unreachable.state != publicReachabilityUnreachable || unreachable.observedAt.IsZero() {
		t.Fatalf("junior external snapshot = %+v", unreachable)
	}
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerPrincipal)
	reachable := adapter.Snapshot(t.Context())
	if reachable.state != publicReachabilityReachable || reachable.observedAt.IsZero() {
		t.Fatalf("principal external snapshot = %+v", reachable)
	}
}

func TestPublicEndpointSnapshotIdentifiesPinnedProbeAndObservationTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = strings.NewReader(
			yagoproto.QueryResponse{Response: 0}.Encode().Encode(),
		).WriteTo(w)
	}))
	defer server.Close()
	probe := newPublicEndpointSelfTest(
		server.Client(),
		"freeworld",
		yagomodel.Hash("AAAAAAAAAAAA"),
		mustURL(t, server.URL),
	)
	wantTime := time.Unix(12, 0)
	probe.pinned = true
	probe.now = func() time.Time { return wantTime }

	snapshot := probe.Snapshot(t.Context())
	if snapshot.state != publicReachabilityReachable ||
		snapshot.source != publicReachabilitySourcePinnedProbe ||
		!snapshot.observedAt.Equal(wantTime) {
		t.Fatalf("pinned snapshot = %+v", snapshot)
	}
	probe.now = nil
	if snapshot = probe.Snapshot(t.Context()); snapshot.observedAt.IsZero() {
		t.Fatalf("default observation time = %+v", snapshot)
	}
}

func TestPublicEndpointSnapshotIsUnknownWithoutProbeTarget(t *testing.T) {
	probe := newPublicEndpointSelfTest(nil, "freeworld", yagomodel.Hash("AAAAAAAAAAAA"), nil)
	snapshot := probe.Snapshot(t.Context())
	if snapshot.state != publicReachabilityUnknown ||
		snapshot.source != publicReachabilitySourceDerivedProbe ||
		!snapshot.observedAt.IsZero() {
		t.Fatalf("targetless snapshot = %+v", snapshot)
	}
}

func TestPublicEndpointSnapshotDoesNotTrustDerivedLocalProbe(t *testing.T) {
	called := false
	probe := newPublicEndpointSelfTest(
		&http.Client{Transport: selfTestRoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true

			return nil, nil
		})},
		"freeworld",
		yagomodel.Hash("AAAAAAAAAAAA"),
		mustURL(t, "http://127.0.0.1:8090"),
	)
	snapshot := probe.Snapshot(t.Context())
	if snapshot.state != publicReachabilityUnknown ||
		snapshot.source != publicReachabilitySourceDerivedProbe || called {
		t.Fatalf("derived local snapshot = %+v, called=%v", snapshot, called)
	}
	probe.pinned = true
	probe.base = nil
	if snapshot = probe.Snapshot(t.Context()); snapshot.state != publicReachabilityUnknown ||
		snapshot.source != publicReachabilitySourcePinnedProbe {
		t.Fatalf("targetless pinned snapshot = %+v", snapshot)
	}
}
