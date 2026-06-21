//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const helloProbeHash = "REMOTEPEER12"

func requireYaCyHash(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
) yacymodel.Hash {
	t.Helper()
	hash, err := resolveYaCyHash(ctx, probe, yacyURL)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func resolveYaCyHash(
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
) (yacymodel.Hash, error) {
	req := yacyproto.HelloRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         yacymodel.Hash(helloProbeHash),
		Seed:        helloProbeSeed(),
	}
	result := probe.Get(ctx, yacyURL+"/yacy/hello.html?"+req.Form().Encode())
	if !result.ok {
		return "", fmt.Errorf("hello request failed")
	}
	msg, err := yacymodel.ParseMessage(result.body)
	if err != nil {
		return "", fmt.Errorf("parse hello response: %w", err)
	}
	resp, err := yacyproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("parse hello response: %w", err)
	}
	seed, ok := resp.OwnSeed().Get()
	if !ok {
		return "", fmt.Errorf("own seed not present in hello response")
	}
	return seed.Hash, nil
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
		Version:  yacymodel.Some("1.83"),
		UTC:      yacymodel.Some(time.Now().UTC().Format("20060102150405")),
		Flags:    yacymodel.Some(flags),
	}
}
