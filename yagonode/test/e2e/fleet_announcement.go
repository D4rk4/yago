//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func announceFleetToYaCy(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	fleet []fleetNode,
) {
	t.Helper()
	for _, node := range fleet {
		seed := fleetAnnouncementSeed(t, node)
		request := yagoproto.HelloRequest{
			NetworkName: yagoproto.DefaultNetwork,
			Seed:        seed,
			Count:       dhtMinConnectedPeers,
			Iam:         node.hash,
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
			t.Fatalf("YaCy never accepted fleet node %s as PeerType=senior", node.alias)
		}
	}
}

func fleetAnnouncementSeed(t *testing.T, node fleetNode) yagomodel.Seed {
	t.Helper()
	host, err := yagomodel.ParseHost(node.alias)
	if err != nil {
		t.Fatalf("parse fleet host %s: %v", node.alias, err)
	}
	port, err := yagomodel.ParsePort(nodeContainerPort)
	if err != nil {
		t.Fatalf("parse fleet port: %v", err)
	}
	flags := yagomodel.ZeroFlags()
	flags = flags.Set(yagomodel.FlagDirectConnect, true)
	flags = flags.Set(yagomodel.FlagAcceptRemoteIndex, true)
	return yagomodel.Seed{
		Hash:     node.hash,
		Name:     yagomodel.Some(node.alias),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(port),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
		Flags:    yagomodel.Some(flags),
		Version:  yagomodel.Some(yagomodel.YaCyVersion("1.83")),
		UTC:      yagomodel.Some(yagomodel.SeedUTCOffsetFromTime(time.Now())),
	}
}
