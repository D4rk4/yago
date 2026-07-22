//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func announceYaCySelfSeedToNode(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	nodeURL string,
) {
	t.Helper()
	result := probe.Get(ctx, yacyURL+"/yacy/seedlist.html?my")
	if !result.ok {
		t.Fatalf("read YaCy self seed: %s", result.diag())
	}
	seed, err := yagomodel.ParseSeedWireForm(ctx, strings.TrimSpace(result.body))
	if err != nil {
		t.Fatalf("parse YaCy self seed: %v", err)
	}
	request := yagoproto.HelloRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Seed:        seed,
		Count:       dhtMinConnectedPeers,
		Iam:         seed.Hash.String(),
	}
	if !waitFor(30*time.Second, func() bool {
		result := probe.PostRaw(
			ctx,
			nodeURL+"/yacy/hello.html",
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
		if err != nil {
			return false
		}

		return response.YourType == yagomodel.PeerSenior ||
			response.YourType == yagomodel.PeerPrincipal
	}) {
		t.Fatal("Yago never admitted the live YaCy self seed")
	}
}
