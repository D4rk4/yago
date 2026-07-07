//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/egressproxy"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/hermeticnetwork"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/nodepeer"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerclient"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerdirectory"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/yacypeer"
)

const (
	yacyAlias = "yacy-e2e"
	nodeAlias = "node-e2e"
	nodeHash  = yacymodel.Hash("ABCDEFGHIJKL")
)

func TestRealYaCyPromotesNodeToSenior(t *testing.T) {
	ctx := context.Background()
	probe := httpprobe.New(t)

	network := hermeticnetwork.New(t, ctx)

	egressproxy.Start(t, ctx, network.Name)

	_, yacyURL := yacypeer.Start(t, ctx, probe, network.Name, yacyAlias)

	nodepeer.Start(t, ctx, probe, nodepeer.Config{
		NetworkName: network.Name,
		Alias:       nodeAlias,
		Hash:        nodeHash,
		SeedlistURL: "http://" + yacyAlias + ":" + peerclient.Port + "/yacy/seedlist.html",
	})

	if !pollwait.For(15*time.Second, func() bool {
		result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000")
		if !result.OK {
			return false
		}
		active, err := peerdirectory.ActivePeerHashes([]byte(result.Body))
		if err != nil {
			return false
		}
		_, ok := active[nodeHash.String()]
		return ok
	}) {
		t.Fatalf("YaCy never saw node hash %s as an active connected peer", nodeHash)
	}

	promoted := pollwait.For(45*time.Second, func() bool {
		result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml")
		if !result.OK {
			return false
		}
		seniors, err := peerdirectory.SeniorHashes([]byte(result.Body))
		if err != nil {
			return false
		}
		_, ok := seniors[nodeHash.String()]
		return ok
	})
	if !promoted {
		if result := probe.Get(ctx, yacyURL+"/yacy/seedlist.xml"); result.OK {
			t.Logf("final seedlist.xml:\n%s", result.Body)
		}
		t.Fatalf("real YaCy never published node hash %s as PeerType=senior", nodeHash)
	}
}
