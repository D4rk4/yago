//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

const dhtMinLocalRWIs = 100

func waitYaCyLocalRWIs(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	yacyHash yacymodel.Hash,
	timeout time.Duration,
) {
	t.Helper()
	last := -1
	if waitFor(timeout, func() bool {
		count, ok := peerQueryCount(ctx, probe, yacyURL, yacyHash, yacyproto.ObjectRWICount)
		if !ok {
			return false
		}
		last = count
		return count >= dhtMinLocalRWIs
	}) {
		return
	}
	t.Fatalf(
		"YaCy never reported at least %d local RWIs (last=%d); DHT sender gate stays closed",
		dhtMinLocalRWIs,
		last,
	)
}
