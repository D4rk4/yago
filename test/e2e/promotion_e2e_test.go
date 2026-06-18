//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	yacyAlias = "yacy-e2e"
	nodeAlias = "node-e2e"
	nodeHash  = yacymodel.Hash("ABCDEFGHIJKL")
)

func TestRealYaCyPromotesNodeToSenior(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	nw := newHermeticNetwork(t, ctx)

	yacyC := startYaCy(t, ctx, nw.Name, yacyAlias)
	yacyURL := hostURL(t, ctx, yacyC)
	if !waitFor(stageYaCyReady, func() bool {
		return probe.OK(ctx, yacyURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("YaCy never became reachable from the host")
	}

	nodeC := startNode(t, ctx, nodeConfig{
		networkName: nw.Name,
		alias:       nodeAlias,
		hash:        nodeHash,
		seedlistURL: "http://" + yacyAlias + ":" + nodeContainerPort + "/yacy/seedlist.html",
	})
	nodeURL := hostURL(t, ctx, nodeC)
	if !waitFor(stageNodeReady, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("node never became reachable from the host")
	}

	if !waitFor(stageHelloHandshake, func() bool {
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

	promoted := waitFor(stageSeniorPromotion, func() bool {
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
