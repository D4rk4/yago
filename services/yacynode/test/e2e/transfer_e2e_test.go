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
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/nodepeer"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerclient"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/yacypeer"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const transferYaCyAlias = "yacy-tr-e2e"

func TestRealYaCyTransfersRWIToFleet(t *testing.T) {
	ctx := context.Background()
	probe := httpprobe.New(t)

	network := hermeticnetwork.New(t, ctx)

	egressproxy.Start(t, ctx, network.Name)

	yacyContainer, yacyURL := yacypeer.Start(t, ctx, probe, network.Name, transferYaCyAlias)

	yacyHash := peerclient.ResolveHash(t, ctx, probe, yacyURL)

	seedlistURL := "http://" + transferYaCyAlias + ":" + peerclient.Port + "/yacy/seedlist.html"
	fleet := nodepeer.StartFleet(
		t,
		ctx,
		probe,
		network.Name,
		seedlistURL,
		nodepeer.MinConnectedPeers,
	)

	yacypeer.PushDocument(t, ctx, probe, yacyURL, yacypeer.TransferTokens())

	yacypeer.WaitLocalRWIs(t, ctx, probe, yacyURL, yacyHash, 30*time.Second)
	waitFleetSenior(t, ctx, probe, yacyURL, fleet, 60*time.Second)
	waitFleetActiveConnected(t, ctx, probe, yacyURL, fleet, 15*time.Second)

	yacyURL = yacypeer.Restart(t, ctx, probe, yacyContainer)

	received := pollwait.For(180*time.Second, func() bool {
		for _, node := range fleet {
			rwiCount, rwiOK := peerclient.QueryCount(
				ctx,
				probe,
				node.URL,
				node.Hash,
				yacyproto.ObjectRWICount,
			)
			urlCount, urlOK := peerclient.QueryCount(
				ctx,
				probe,
				node.URL,
				node.Hash,
				yacyproto.ObjectLURLCount,
			)
			if rwiOK && urlOK && rwiCount > 0 && urlCount > 0 {
				t.Logf(
					"fleet node %s received transferred RWIs: rwicount=%d urlcount=%d",
					node.Alias,
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
