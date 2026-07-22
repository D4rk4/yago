//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func announceIndexTransferSender(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
) {
	t.Helper()
	port, err := yagomodel.ParsePort(nodeContainerPort)
	if err != nil {
		t.Fatalf("parse transfer sender port: %v", err)
	}
	flags := yagomodel.ZeroFlags()
	flags = flags.Set(yagomodel.FlagDirectConnect, true)
	flags = flags.Set(yagomodel.FlagAcceptRemoteIndex, true)
	request := yagoproto.HelloRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Seed: yagomodel.Seed{
			Hash:     seededPeerHash,
			Name:     yagomodel.Some("e2e-index-transfer-sender"),
			Port:     yagomodel.Some(port),
			PeerType: yagomodel.Some(yagomodel.PeerSenior),
			Flags:    yagomodel.Some(flags),
			Version:  yagomodel.Some(yagomodel.YaCyVersion("1.83")),
			UTC:      yagomodel.Some(yagomodel.SeedUTCOffsetFromTime(time.Now())),
		},
		Iam: seededPeerHash.String(),
	}
	result := probe.PostRaw(
		ctx,
		nodeURL+"/yacy/hello.html",
		request.Form().Encode(),
		"Content-Type: application/x-www-form-urlencoded",
	)
	if !result.ok {
		t.Fatalf("announce index transfer sender: %s", result.diag())
	}
	message, err := yagomodel.ParseMessage(result.body)
	if err != nil {
		t.Fatalf("parse transfer sender hello response: %v", err)
	}
	response, err := yagoproto.ParseHelloResponse(ctx, message)
	if err != nil {
		t.Fatalf("parse transfer sender hello response: %v", err)
	}
	if response.YourType == yagomodel.PeerVirgin {
		t.Fatal("node did not admit the index transfer sender")
	}
}
