//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/nodepeer"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerdirectory"
)

func waitFleetSenior(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	yacyURL string,
	fleet []nodepeer.Peer,
	timeout time.Duration,
) {
	t.Helper()
	if pollwait.For(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml")
		if !result.OK {
			return false
		}
		seniors, err := peerdirectory.SeniorHashes([]byte(result.Body))
		if err != nil {
			return false
		}
		for _, node := range fleet {
			if _, ok := seniors[node.Hash.String()]; !ok {
				return false
			}
		}
		return true
	}) {
		return
	}
	if result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml"); result.OK {
		t.Logf("final seedlist.xml:\n%s", result.Body)
	}
	t.Fatalf("YaCy never published all %d fleet hashes as PeerType=senior", len(fleet))
}

func waitFleetActiveConnected(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	yacyURL string,
	fleet []nodepeer.Peer,
	timeout time.Duration,
) {
	t.Helper()
	hashes := make(map[string]struct{}, len(fleet))
	for _, node := range fleet {
		hashes[node.Hash.String()] = struct{}{}
	}
	if pollwait.For(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000")
		if !result.OK {
			return false
		}
		active, err := peerdirectory.ActivePeerHashes([]byte(result.Body))
		if err != nil {
			return false
		}
		for hash := range active {
			if _, ok := hashes[hash]; ok {
				return true
			}
		}
		return false
	}) {
		return
	}
	t.Fatal("YaCy has no active connected fleet node; DHT dispatcher may remain nil after restart")
}
