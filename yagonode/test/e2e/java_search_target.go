//go:build e2e

package e2e

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func announceNodeSelfSeedToYaCy(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	yacyURL string,
	nodeHash yagomodel.Hash,
) {
	t.Helper()
	result := probe.Get(ctx, nodeURL+"/yacy/seedlist.html?my")
	if !result.ok {
		t.Fatalf("read Yago self seed: %s", result.diag())
	}
	seed, err := parseNodeSelfSeed(ctx, result.body)
	if err != nil {
		t.Fatalf("parse Yago self seed: %v", err)
	}
	request := yagoproto.HelloRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Seed:        seed,
		Count:       dhtMinConnectedPeers,
		Iam:         nodeHash.String(),
	}
	if !waitFor(30*time.Second, func() bool {
		result := probe.PostRaw(
			ctx,
			yacyURL+"/yacy/hello.html",
			request.Form().Encode(),
			"Content-Type: application/x-www-form-urlencoded",
		)
		if !result.ok {
			return false
		}
		message, err := yagomodel.ParseMessage(result.body)
		if err != nil {
			return false
		}
		response, err := yagoproto.ParseHelloResponse(ctx, message)
		return err == nil && response.YourType == yagomodel.PeerSenior
	}) {
		t.Fatal("real YaCy never accepted the live Yago self seed")
	}
}

func parseNodeSelfSeed(ctx context.Context, body string) (yagomodel.Seed, error) {
	return yagomodel.ParseSeedWireForm(ctx, strings.TrimSpace(body))
}

func waitYaCySearchTarget(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	nodeHash yagomodel.Hash,
	timeout time.Duration,
) {
	t.Helper()
	if waitFor(timeout, func() bool {
		result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000")
		if !result.ok {
			return false
		}
		state, found, err := activeYaCyPeerState([]byte(result.body), nodeHash.String())
		return err == nil && found && state.Words >= 1 && state.Age >= 3 && state.DHTReceive == 1
	}) {
		return
	}
	if result := probe.Get(ctx, yacyURL+"/Network.xml?page=1&maxCount=1000"); result.ok {
		t.Logf("final real YaCy network view:\n%s", result.body)
	}
	t.Fatalf("real YaCy never exposed Yago %s as an eligible remote-search target", nodeHash)
}

type activeYaCyPeer struct {
	Hash       string `xml:"hash"`
	Words      int    `xml:"words"`
	Age        int    `xml:"age"`
	DHTReceive int    `xml:"dhtreceive"`
}

func activeYaCyPeerState(body []byte, hash string) (activeYaCyPeer, bool, error) {
	var peers struct {
		Peers []activeYaCyPeer `xml:"peer"`
	}
	if err := xml.Unmarshal(body, &peers); err != nil {
		return activeYaCyPeer{}, false, err
	}
	for _, peer := range peers.Peers {
		if peer.Hash == hash {
			return peer, true, nil
		}
	}
	return activeYaCyPeer{}, false, nil
}
