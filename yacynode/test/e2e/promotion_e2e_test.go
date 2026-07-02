//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

const (
	yacyAlias = "yacy-e2e"
	nodeAlias = "node-e2e"
	nodeHash  = yacymodel.Hash("ABCDEFGHIJKL")
)

func TestRealYaCyPromotesNodeToSenior(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	network := newHermeticNetwork(t, ctx)

	startEgressProxy(t, ctx, network.Name)

	_, yacyURL := startYaCy(t, ctx, probe, network.Name, yacyAlias)

	startNode(t, ctx, probe, nodeConfig{
		networkName: network.Name,
		alias:       nodeAlias,
		hash:        nodeHash,
		seedlistURL: "http://" + yacyAlias + ":" + nodeContainerPort + "/yacy/seedlist.html",
	})

	if !waitFor(15*time.Second, func() bool {
		result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000")
		if !result.ok {
			return false
		}
		active, err := networkActivePeerHashes([]byte(result.body))
		if err != nil {
			return false
		}
		_, ok := active[nodeHash.String()]
		return ok
	}) {
		t.Fatalf("YaCy never saw node hash %s as an active connected peer", nodeHash)
	}

	promoted := waitFor(45*time.Second, func() bool {
		result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml")
		if !result.ok {
			return false
		}
		seniors, err := seedlistSeniorHashes([]byte(result.body))
		if err != nil {
			return false
		}
		_, ok := seniors[nodeHash.String()]
		return ok
	})
	if !promoted {
		if result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml"); result.ok {
			t.Logf("final seedlist.xml:\n%s", result.body)
		}
		t.Fatalf("real YaCy never published node hash %s as PeerType=senior", nodeHash)
	}
}
