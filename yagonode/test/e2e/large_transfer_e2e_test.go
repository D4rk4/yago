//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	largeTransferYaCyAlias = "yacy-lt-e2e"
	largeTransferNodeAlias = "node-lt-e2e"
	largeTransferNodeHash  = yagomodel.Hash("EFGHIJKLMNOP")
)

func TestNodeDeliversURLTransferAboveJavaFormLimitToRealYaCy(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	probeNetwork := newHermeticNetwork(t, ctx)
	observerNetwork := newExternalObserverNetwork(t, ctx)

	_, yacyURL := startYaCyWithObserverNetwork(
		t,
		ctx,
		probe,
		probeNetwork.Name,
		observerNetwork.Name,
		largeTransferYaCyAlias,
	)
	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	_, nodeURL := startNode(t, ctx, probe, nodeConfig{
		networkName:         probeNetwork.Name,
		observerNetworkName: observerNetwork.Name,
		alias:               largeTransferNodeAlias,
		hash:                largeTransferNodeHash,
		seedlistURL:         "http://" + largeTransferYaCyAlias + ":8090/yacy/seedlist.html",
		extraEnv: map[string]string{
			"YAGO_DHT_MINIMUM_PEER_AGE_DAYS":   "-1",
			"YAGO_DHT_MINIMUM_CONNECTED_PEERS": "1",
			"YAGO_DHT_MINIMUM_RWI_WORDS":       "1",
			"YAGO_DHT_DISTRIBUTION_INTERVAL":   "5s",
			"YAGO_PUBLIC_SELF_TEST_URL":        "http://" + largeTransferNodeAlias + ":8090",
		},
	})
	admitYaCyToFleetNodes(t, ctx, probe, yacyURL, []fleetNode{{
		alias: largeTransferNodeAlias,
		hash:  largeTransferNodeHash,
		url:   nodeURL,
	}})

	documents := buildLargeTransferDocuments(t)
	word := yagomodel.WordHash("yagolargetransferword")
	seedNodeLargeTransfer(t, ctx, probe, nodeURL, largeTransferNodeHash, word, documents)
	urlHashes := make([]yagomodel.Hash, len(documents))
	for position, document := range documents {
		urlHashes[position] = document.urlHash.Hash()
	}

	stored := 0
	received := waitForEvery(180*time.Second, time.Second, func() bool {
		postings, postingsOK := peerQueryCountWithEnv(
			ctx,
			probe,
			yacyURL,
			yacyHash,
			yagoproto.ObjectRWIURLCount,
			word.String(),
		)
		stored = len(yaCyStoredURLMetadata(
			ctx,
			probe,
			yacyURL,
			largeTransferNodeHash,
			yacyHash,
			urlHashes,
		))

		return postingsOK && postings > 0 && stored == len(urlHashes)
	})
	if !received {
		t.Fatalf(
			"real YaCy stored %d of %d URL metadata rows sent in one transfer above %d bytes",
			stored,
			len(urlHashes),
			javaFormContentLimitBytes,
		)
	}
}
