//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const seededPeerHash = yagomodel.Hash("QRSTUVWXYZab")

func seedNodeIndex(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yagomodel.Hash,
	words []string,
	pageURL string,
) {
	t.Helper()
	announceIndexTransferSender(t, ctx, probe, nodeURL)
	urlHash, err := yagomodel.HashURL(pageURL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}

	lines := make([]string, len(words))
	for i, word := range words {
		lines[i] = fmt.Sprintf(
			"%s{c=1,h=%s,t=1,x=2,z=AAAAAA}",
			yagomodel.WordHash(word),
			urlHash,
		)
	}

	postNodeRWILines(t, ctx, probe, nodeURL, nodeHash, len(words), lines)
	postNodeURLRows(t, ctx, probe, nodeURL, nodeHash, []string{
		seededURLRow(urlHash, pageURL, "Transfer Interop Document", len(words)),
	})
}
