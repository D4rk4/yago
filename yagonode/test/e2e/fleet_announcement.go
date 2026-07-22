//go:build e2e

package e2e

import (
	"context"
	"testing"
)

func admitYaCyToFleetNodes(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	fleet []fleetNode,
) {
	t.Helper()
	for _, node := range fleet {
		announceYaCySelfSeedToNode(t, ctx, probe, yacyURL, node.url)
	}
}
