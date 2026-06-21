//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const transferYaCyAlias = "yacy-tr-e2e"

func TestRealYaCyTransfersRWIToFleet(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	network := newHermeticNetwork(t, ctx)

	yacyContainer, yacyURL := startYaCy(t, ctx, probe, network.Name, transferYaCyAlias)

	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	seedlistURL := "http://" + transferYaCyAlias + ":" + nodeContainerPort + "/yacy/seedlist.html"
	fleet := startNodeFleet(t, ctx, probe, network.Name, seedlistURL, dhtMinConnectedPeers)

	pushDocument(t, ctx, probe, yacyURL, buildTransferTokens())

	waitYaCyLocalRWIs(t, ctx, probe, yacyURL, yacyHash, 30*time.Second)
	waitFleetSenior(t, ctx, probe, yacyURL, fleet, 60*time.Second)
	waitFleetActiveConnected(t, ctx, probe, yacyURL, fleet, 15*time.Second)

	yacyURL = restartYaCy(t, ctx, probe, yacyContainer)

	received := waitFor(180*time.Second, func() bool {
		for _, node := range fleet {
			rwiCount, rwiOK := peerQueryCount(
				ctx,
				probe,
				node.url,
				node.hash,
				yacyproto.ObjectRWICount,
			)
			urlCount, urlOK := peerQueryCount(
				ctx,
				probe,
				node.url,
				node.hash,
				yacyproto.ObjectLURLCount,
			)
			if rwiOK && urlOK && rwiCount > 0 && urlCount > 0 {
				t.Logf(
					"fleet node %s received transferred RWIs: rwicount=%d urlcount=%d",
					node.alias,
					rwiCount,
					urlCount,
				)
				return true
			}
		}
		return false
	})
	if !received {
		t.Fatal("no fleet node received transferred RWIs with URL references from real YaCy")
	}
}
