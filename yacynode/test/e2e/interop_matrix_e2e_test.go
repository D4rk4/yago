//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	outboundYaCyAlias = "yacy-out-e2e"
	outboundNodeAlias = "node-out-e2e"
	outboundNodeHash  = yacymodel.Hash("BCDEFGHIJKLM")
	searchYaCyAlias   = "yacy-se-e2e"
	searchNodeAlias   = "node-se-e2e"
	searchNodeHash    = yacymodel.Hash("CDEFGHIJKLMN")
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
	startEgressProxy(t, ctx, network.Name)

	_, yacyURL := startYaCy(t, ctx, probe, network.Name, outboundYaCyAlias)
	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	_, nodeURL := startNode(t, ctx, probe, nodeConfig{
		networkName: network.Name,
		alias:       outboundNodeAlias,
		hash:        outboundNodeHash,
		seedlistURL: "http://" + outboundYaCyAlias + ":8090/yacy/seedlist.html",
		extraEnv: map[string]string{
			"YACY_DHT_MINIMUM_PEER_AGE_DAYS":   "-1",
			"YACY_DHT_MINIMUM_CONNECTED_PEERS": "1",
			"YACY_DHT_MINIMUM_RWI_WORDS":       "50",
			"YACY_DHT_DISTRIBUTION_INTERVAL":   "5s",
			"YACY_PUBLIC_SELF_TEST_URL":        "http://" + outboundNodeAlias + ":8090",
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

	baseline, ok := peerQueryCount(ctx, probe, yacyURL, yacyHash, yacyproto.ObjectRWICount)
	if !ok {
		t.Fatal("real YaCy did not answer its rwicount probe")
	}

	received := waitFor(180*time.Second, func() bool {
		count, ok := peerQueryCount(ctx, probe, yacyURL, yacyHash, yacyproto.ObjectRWICount)
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
	startEgressProxy(t, ctx, network.Name)

	_, yacyURL := startYaCy(t, ctx, probe, network.Name, searchYaCyAlias)
	yacyHash := resolveYaCyHash(t, ctx, probe, yacyURL)

	tokens := buildTransferTokens()
	pushDocument(t, ctx, probe, yacyURL, tokens)
	waitYaCyLocalRWIs(t, ctx, probe, yacyURL, yacyHash, 30*time.Second)

	_, nodeURL := startNode(t, ctx, probe, nodeConfig{
		networkName: network.Name,
		alias:       searchNodeAlias,
		hash:        searchNodeHash,
		seedlistURL: "http://" + searchYaCyAlias + ":8090/yacy/seedlist.html",
		extraEnv: map[string]string{
			"YACY_DHT_MINIMUM_PEER_AGE_DAYS": "-1",
		},
	})

	searchURL := nodeURL + "/yacysearch.json?" + url.Values{
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
