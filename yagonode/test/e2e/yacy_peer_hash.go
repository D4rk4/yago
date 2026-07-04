//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const helloProbeHash = "REMOTEPEER12"

func resolveYaCyHash(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
) yagomodel.Hash {
	t.Helper()
	req := yagoproto.HelloRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Iam:         yagomodel.Hash(helloProbeHash),
		Seed:        helloProbeSeed(),
	}
	result := probe.Get(ctx, yacyURL+"/yacy/hello.html?"+req.Form().Encode())
	if !result.ok {
		t.Fatal("hello request failed")
	}
	msg, err := yagomodel.ParseMessage(result.body)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	resp, err := yagoproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	seed, ok := resp.OwnSeed().Get()
	if !ok {
		t.Fatal("own seed not present in hello response")
	}
	return seed.Hash
}

func helloProbeSeed() yagomodel.Seed {
	flags := yagomodel.ZeroFlags()
	flags = flags.Set(yagomodel.FlagDirectConnect, true)
	flags = flags.Set(yagomodel.FlagAcceptRemoteIndex, true)
	port, _ := yagomodel.ParsePort(nodeContainerPort)
	return yagomodel.Seed{
		Hash:     yagomodel.Hash(helloProbeHash),
		Name:     yagomodel.Some("e2e-hello-probe"),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
		Port:     yagomodel.Some(port),
		Version:  yagomodel.Some(yagomodel.YaCyVersion("1.83")),
		UTC:      yagomodel.Some(yagomodel.SeedUTCOffsetFromTime(time.Now())),
		Flags:    yagomodel.Some(flags),
	}
}
