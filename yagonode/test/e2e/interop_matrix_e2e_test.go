//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	outboundYaCyAlias = "yacy-out-e2e"
	outboundNodeAlias = "node-out-e2e"
	outboundNodeHash  = yagomodel.Hash("BCDEFGHIJKLM")
	searchYaCyAlias   = "yacy-se-e2e"
	searchNodeAlias   = "node-se-e2e"
	searchNodeHash    = yagomodel.Hash("CDEFGHIJKLMN")
)

func buildOutboundWords() []string {
	words := make([]string, 120)
	for i := range words {
		words[i] = fmt.Sprintf("yagooutbounduniqueword%03d", i)
	}
	return words
}

func TestNodeDistributesRWIToRealYaCy(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	network := newHermeticNetwork(t, ctx)

	_, yacyURL := startYaCy(t, ctx, probe, network.Name, outboundYaCyAlias)
	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	_, nodeURL := startNode(t, ctx, probe, nodeConfig{
		networkName: network.Name,
		alias:       outboundNodeAlias,
		hash:        outboundNodeHash,
		seedlistURL: "http://" + outboundYaCyAlias + ":8090/yacy/seedlist.html",
		extraEnv: map[string]string{
			"YAGO_DHT_MINIMUM_PEER_AGE_DAYS":   "-1",
			"YAGO_DHT_MINIMUM_CONNECTED_PEERS": "1",
			"YAGO_DHT_MINIMUM_RWI_WORDS":       "50",
			"YAGO_DHT_DISTRIBUTION_INTERVAL":   "5s",
			"YAGO_PUBLIC_SELF_TEST_URL":        "http://" + outboundNodeAlias + ":8090",
		},
	})

	seedNodeIndex(
		t,
		ctx,
		probe,
		nodeURL,
		outboundNodeHash,
		buildOutboundWords(),
		"http://outbound.example.invalid/doc.txt",
	)

	baseline, ok := peerQueryCount(ctx, probe, yacyURL, yacyHash, yagoproto.ObjectRWICount)
	if !ok {
		t.Fatal("real YaCy did not answer its rwicount probe")
	}

	received := waitFor(180*time.Second, func() bool {
		count, ok := peerQueryCount(ctx, probe, yacyURL, yacyHash, yagoproto.ObjectRWICount)
		return ok && count > baseline
	})
	if !received {
		t.Fatalf(
			"real YaCy rwicount never grew above baseline %d after node DHT handoff",
			baseline,
		)
	}
}

func TestGlobalSearchFindsRealYaCyResults(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)

	network := newHermeticNetwork(t, ctx)

	_, yacyURL := startYaCy(t, ctx, probe, network.Name, searchYaCyAlias)
	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	tokens := buildTransferTokens()
	pushDocument(t, ctx, probe, yacyURL, tokens)
	waitYaCyLocalRWIs(t, ctx, probe, yacyURL, yacyHash, 30*time.Second)

	nodeContainer, _ := startNode(t, ctx, probe, nodeConfig{
		networkName: network.Name,
		alias:       searchNodeAlias,
		hash:        searchNodeHash,
		seedlistURL: "http://" + searchYaCyAlias + ":8090/yacy/seedlist.html",
		extraEnv: map[string]string{
			"YAGO_DHT_MINIMUM_PEER_AGE_DAYS": "-1",
		},
	})

	// The client search surfaces now live on the dedicated public listener, not
	// the peer port that carries the /yacy/* wire protocol.
	searchURL := nodePublicURL(t, ctx, nodeContainer) + "/yacysearch.json?" + url.Values{
		"query":          {tokens[0]},
		"resource":       {"global"},
		"maximumRecords": {"10"},
	}.Encode()

	found := waitFor(120*time.Second, func() bool {
		result := probe.Get(ctx, searchURL)
		return result.ok && strings.Contains(result.body, "transfer.example.invalid")
	})
	if !found {
		if result := probe.Get(ctx, searchURL); result.ok {
			t.Logf("final global search response:\n%s", result.body)
		}
		t.Fatal("global search never returned the document indexed by real YaCy")
	}
}
