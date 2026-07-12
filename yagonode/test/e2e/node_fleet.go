//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const dhtMinConnectedPeers = 33

type fleetNode struct {
	alias string
	hash  yagomodel.Hash
	url   string
}

func startNodeFleet(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	networkName, seedlistURL string,
	size int,
) []fleetNode {
	t.Helper()
	fleet := make([]fleetNode, size)
	for i := range fleet {
		alias := fmt.Sprintf("node-tr-%02d", i)
		hash, err := yagomodel.NewHash()
		if err != nil {
			t.Fatalf("generate node hash: %v", err)
		}
		_, url := startNode(t, ctx, probe, nodeConfig{
			networkName: networkName,
			alias:       alias,
			hash:        hash,
			seedlistURL: seedlistURL,
			extraEnv: map[string]string{
				"YAGO_ANNOUNCE_INTERVAL": "1h",
				"YAGO_GREETS_PER_CYCLE":  "1",
			},
		})
		fleet[i] = fleetNode{alias: alias, hash: hash, url: url}
	}
	return fleet
}
