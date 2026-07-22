package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

type scriptedOverviewPeerType struct {
	peerType yagomodel.PeerType
}

func (s scriptedOverviewPeerType) PublishedPeerType(context.Context) yagomodel.PeerType {
	return s.peerType
}

func TestOverviewDefaultsToUnknownWithoutExternalClassification(t *testing.T) {
	overview := newOverviewSource(stubReport{}).Overview(t.Context())
	if overview.PeerType != "unknown" {
		t.Fatalf("peer type = %q, want unknown", overview.PeerType)
	}
}

func TestOverviewUsesPublishedExternalClassification(t *testing.T) {
	for _, peerType := range []yagomodel.PeerType{
		yagomodel.PeerSenior,
		yagomodel.PeerPrincipal,
	} {
		t.Run(peerType.String(), func(t *testing.T) {
			overview := newOverviewSource(stubReport{}).
				withPeerType(scriptedOverviewPeerType{peerType: peerType}).
				Overview(t.Context())
			if overview.PeerType != yagomodel.PeerSenior.String() {
				t.Fatalf("peer type = %q, want senior", overview.PeerType)
			}
		})
	}
}

func TestOverviewShowsPublishedVirginAtStartup(t *testing.T) {
	overview := newOverviewSource(stubReport{}).
		withPeerType(scriptedOverviewPeerType{peerType: yagomodel.PeerVirgin}).
		Overview(t.Context())
	if overview.PeerType != yagomodel.PeerVirgin.String() {
		t.Fatalf("peer type = %q, want virgin", overview.PeerType)
	}
}

func TestOverviewUsesJuniorAndRejectsInvalidPublishedClassification(t *testing.T) {
	junior := newOverviewSource(stubReport{}).
		withPeerType(scriptedOverviewPeerType{peerType: yagomodel.PeerJunior}).
		Overview(t.Context())
	if junior.PeerType != yagomodel.PeerJunior.String() {
		t.Fatalf("junior peer type = %q", junior.PeerType)
	}
	invalid := newOverviewSource(stubReport{}).
		withPeerType(scriptedOverviewPeerType{peerType: yagomodel.PeerType("mentor")}).
		Overview(t.Context())
	if invalid.PeerType != "unknown" {
		t.Fatalf("invalid peer type = %q", invalid.PeerType)
	}
}

func TestOverviewTracksExternalHelloClassification(t *testing.T) {
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	observer := yagomodel.Hash("ABCDEFGHIJKL")

	overview := newOverviewSource(stubReport{}).withPeerType(evidence).Overview(t.Context())
	if overview.PeerType != yagomodel.PeerVirgin.String() {
		t.Fatalf("initial peer type = %q, want virgin", overview.PeerType)
	}
	evidence.Observe(observer, yagomodel.PeerSenior)

	overview = newOverviewSource(stubReport{}).withPeerType(evidence).Overview(t.Context())
	if overview.PeerType != yagomodel.PeerSenior.String() {
		t.Fatalf("peer type = %q, want senior", overview.PeerType)
	}

	evidence.Observe(observer, yagomodel.PeerJunior)
	overview = newOverviewSource(stubReport{}).withPeerType(evidence).Overview(t.Context())
	if overview.PeerType != yagomodel.PeerJunior.String() {
		t.Fatalf("peer type = %q, want junior", overview.PeerType)
	}
}
