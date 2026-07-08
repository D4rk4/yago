//go:build e2e

package nodepeer

import (
	"context"
	"fmt"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Peer struct {
	Alias string
	Hash  yacymodel.Hash
	URL   string
}

func StartFleet(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	networkName, seedlistURL string,
	size int,
) []Peer {
	t.Helper()
	fleet := make([]Peer, size)
	for i := range fleet {
		alias := fmt.Sprintf("node-tr-%02d", i)
		hash, err := yacymodel.NewHash()
		if err != nil {
			t.Fatalf("generate node hash: %v", err)
		}
		_, url := Start(t, ctx, probe, Config{
			NetworkName: networkName,
			Alias:       alias,
			Hash:        hash,
			SeedlistURL: seedlistURL,
		})
		fleet[i] = Peer{Alias: alias, Hash: hash, URL: url}
	}
	return fleet
}
