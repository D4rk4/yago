//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const helloProbeHash = "REMOTEPEER12"

func resolveYaCyHash(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
) yacymodel.Hash {
	t.Helper()
	req := yacyproto.HelloRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         yacymodel.Hash(helloProbeHash),
		Seed:        helloProbeSeed(),
	}
	result := probe.Get(ctx, yacyURL+"/yacy/hello.html?"+req.Form().Encode())
	if !result.ok {
		t.Fatal("hello request failed")
	}
	msg, err := yacymodel.ParseMessage(result.body)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	resp, err := yacyproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	seed, ok := resp.OwnSeed().Get()
	if !ok {
		t.Fatal("own seed not present in hello response")
	}
	return seed.Hash
}

func helloProbeSeed() yacymodel.Seed {
	flags := yacymodel.ZeroFlags()
	flags = flags.Set(yacymodel.FlagDirectConnect, true)
	flags = flags.Set(yacymodel.FlagAcceptRemoteIndex, true)
	port, _ := yacymodel.ParsePort(nodeContainerPort)
	return yacymodel.Seed{
		Hash:     yacymodel.Hash(helloProbeHash),
		Name:     yacymodel.Some("e2e-hello-probe"),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
		Port:     yacymodel.Some(port),
		Version:  yacymodel.Some(yacymodel.YaCyVersion("1.83")),
		UTC:      yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(time.Now())),
		Flags:    yacymodel.Some(flags),
	}
}
