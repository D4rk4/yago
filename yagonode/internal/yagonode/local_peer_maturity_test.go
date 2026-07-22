package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestLocalPeerMaturityUsesPublishedSelfType(t *testing.T) {
	tests := []struct {
		name     string
		report   stubReport
		missing  bool
		isVirgin bool
	}{
		{name: "missing report", missing: true, isVirgin: true},
		{name: "missing type", report: stubReport{}, isVirgin: true},
		{name: "virgin", report: reportWithPeerType(yagomodel.PeerVirgin), isVirgin: true},
		{name: "junior", report: reportWithPeerType(yagomodel.PeerJunior)},
		{name: "senior", report: reportWithPeerType(yagomodel.PeerSenior)},
		{name: "principal", report: reportWithPeerType(yagomodel.PeerPrincipal)},
		{name: "unsupported", report: reportWithPeerType(yagomodel.PeerMentor), isVirgin: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.missing {
				if !localPeerIsVirgin(t.Context(), nil) {
					t.Fatal("missing report was treated as mature")
				}

				return
			}
			if got := localPeerIsVirgin(t.Context(), test.report); got != test.isVirgin {
				t.Fatalf("is virgin = %t, want %t", got, test.isVirgin)
			}
		})
	}
}

func reportWithPeerType(peerType yagomodel.PeerType) stubReport {
	return stubReport{seed: yagomodel.Seed{PeerType: yagomodel.Some(peerType)}}
}
