package rwi

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

type rwiPorts struct {
	Directory PostingDirectory
	Index     PostingScanner
	Receiver  PostingReceiver
}

type harness struct {
	vault *boltvault.Vault
	urls  urlmeta.URLReceiver
	rwi   rwiPorts
}

func openHarness(t *testing.T, quotaBytes int64, batchCap int) harness {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	directory, _, urlReceiver, err := urlmeta.Open(vault)
	if err != nil {
		t.Fatalf("urlmeta.Open: %v", err)
	}
	index, receiver, err := Open(
		vault,
		directory,
		Config{BatchCap: batchCap, PauseSeconds: 5},
	)
	if err != nil {
		t.Fatalf("rwi.Open: %v", err)
	}

	return harness{
		vault: vault,
		urls:  urlReceiver,
		rwi:   rwiPorts{Directory: index, Index: index, Receiver: receiver},
	}
}

func posting(word, urlSeed string) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: yacymodel.WordHash(word),
		Properties: map[string]string{
			yacymodel.ColURLHash:        yacymodel.WordHash(urlSeed).String(),
			yacymodel.ColLocalLinkCount: "1",
			yacymodel.ColHitCount:       "1",
			yacymodel.ColWordDistance:   "0",
		},
	}
}

func urlRow(seed string) yacymodel.URIMetadataRow {
	return yacymodel.URIMetadataRow{
		Properties: map[string]string{yacymodel.URLMetaHash: yacymodel.WordHash(seed).String()},
	}
}

func referencedHash(t *testing.T, entry yacymodel.RWIPosting) yacymodel.Hash {
	t.Helper()

	urlHash, err := entry.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}

	return urlHash.Hash()
}

func TestIntakePersistsAndCounts(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.urls.Receive(
		ctx,
		[]yacymodel.URIMetadataRow{urlRow("u1"), urlRow("u2")},
	); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w1", "u1"),
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy || len(receipt.UnknownURL) != 0 {
		t.Fatalf("receipt = %+v, want empty", receipt)
	}

	rwiCount, err := h.rwi.Directory.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 2 {
		t.Fatalf("RWICount = %d, want 2", rwiCount)
	}

	refCount, err := h.rwi.Directory.ReferencedURLCount(ctx)
	if err != nil {
		t.Fatalf("ReferencedURLCount: %v", err)
	}
	if refCount != 2 {
		t.Fatalf("ReferencedURLCount = %d, want 2", refCount)
	}
}

func TestIntakeReportsUnknownURL(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)
	entry := posting("w1", "u1")

	receipt, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{entry})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if len(receipt.UnknownURL) != 1 || receipt.UnknownURL[0] != referencedHash(t, entry) {
		t.Fatalf("UnknownURL = %v, want the referenced hash", receipt.UnknownURL)
	}

	if _, err := h.urls.Receive(ctx, []yacymodel.URIMetadataRow{urlRow("u1")}); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err = h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{entry})
	if err != nil {
		t.Fatalf("Intake known: %v", err)
	}
	if len(receipt.UnknownURL) != 0 {
		t.Fatalf("UnknownURL = %v, want empty after url known", receipt.UnknownURL)
	}
}

func TestIntakeBusyAtCapacity(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 1, 100)

	receipt, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{posting("w1", "u1")})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if !receipt.Busy || receipt.Pause != 5 {
		t.Fatalf("receipt = %+v, want Busy with pause 5", receipt)
	}
}

func TestIntakeBusyOverBatchCap(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 1)

	receipt, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want Busy over batch cap", receipt)
	}
}
