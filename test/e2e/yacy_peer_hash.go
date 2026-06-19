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

func requireYaCyHash(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
) yacymodel.Hash {
	t.Helper()
	hash, ok := resolveYaCyHash(ctx, probe, yacyURL)
	if !ok {
		t.Fatal("could not resolve real YaCy own peer hash via hello handshake")
	}
	return hash
}

func resolveYaCyHash(ctx context.Context, probe *httpProbe, yacyURL string) (yacymodel.Hash, bool) {
	req := yacyproto.HelloRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         yacymodel.Hash(helloProbeHash),
		Seed:        helloProbeSeed(),
	}
	result := probe.Get(ctx, yacyURL+"/yacy/hello.html?"+req.Form().Encode())
	if !result.ok {
		return "", false
	}
	msg, err := yacymodel.ParseMessage(result.body)
	if err != nil {
		return "", false
	}
	resp, err := yacyproto.ParseHelloResponse(msg)
	if err != nil {
		return "", false
	}
	hash, err := resp.OwnSeed().Hash()
	if err != nil {
		return "", false
	}
	return hash, true
}

func helloProbeSeed() yacymodel.Seed {
	flags := yacymodel.ZeroFlags()
	flags = flags.Set(yacymodel.FlagDirectConnect, true)
	flags = flags.Set(yacymodel.FlagAcceptRemoteIndex, true)
	return yacymodel.Seed{
		yacymodel.SeedHash:     helloProbeHash,
		yacymodel.SeedName:     "e2e-hello-probe",
		yacymodel.SeedPeerType: "senior",
		yacymodel.SeedPort:     nodeContainerPort,
		yacymodel.SeedVersion:  "1.83",
		yacymodel.SeedUTC:      time.Now().UTC().Format("20060102150405"),
		yacymodel.SeedFlags:    flags.String(),
	}
}
