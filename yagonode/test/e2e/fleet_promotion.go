//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

func waitFleetSenior(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	fleet []fleetNode,
	timeout time.Duration,
) {
	t.Helper()
	if waitFor(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml")
		if !result.ok {
			return false
		}
		seniors, err := seedlistSeniorHashes([]byte(result.body))
		if err != nil {
			return false
		}
		for _, node := range fleet {
			if _, ok := seniors[node.hash.String()]; !ok {
				return false
			}
		}
		return true
	}) {
		return
	}
	if result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml"); result.ok {
		t.Logf("final seedlist.xml:\n%s", result.body)
	}
	t.Fatalf("YaCy never published all %d fleet hashes as PeerType=senior", len(fleet))
}

func waitFleetActiveConnected(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	fleet []fleetNode,
	timeout time.Duration,
) {
	t.Helper()
	if waitFor(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000")
		if !result.ok {
			return false
		}
		active, err := networkActivePeerHashes([]byte(result.body))
		if err != nil {
			return false
		}
		for _, node := range fleet {
			if _, ok := active[node.hash.String()]; !ok {
				return false
			}
		}

		return true
	}) {
		return
	}
	if result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000"); result.ok {
		t.Logf("final real YaCy network view:\n%s", result.body)
	}
	t.Fatalf("YaCy never retained all %d fleet hashes as active connected peers", len(fleet))
}

func waitYaCySelfSenior(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	yacyHash string,
	timeout time.Duration,
) {
	t.Helper()
	if waitFor(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml?my")
		if !result.ok {
			return false
		}
		seniors, err := seedlistSeniorHashes([]byte(result.body))
		if err != nil {
			return false
		}
		_, senior := seniors[yacyHash]

		return senior
	}) {
		return
	}
	if result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml?my"); result.ok {
		t.Logf("final YaCy self seed:\n%s", result.body)
	}
	t.Fatalf("YaCy self seed %s never became PeerType=senior", yacyHash)
}
