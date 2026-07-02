//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

const dhtMinConnectedPeers = 33

type fleetNode struct {
	alias string
	hash  yacymodel.Hash
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
		hash, err := yacymodel.NewHash()
		if err != nil {
			t.Fatalf("generate node hash: %v", err)
		}
		_, url := startNode(t, ctx, probe, nodeConfig{
			networkName: networkName,
			alias:       alias,
			hash:        hash,
			seedlistURL: seedlistURL,
		})
		fleet[i] = fleetNode{alias: alias, hash: hash, url: url}
	}
	return fleet
}
