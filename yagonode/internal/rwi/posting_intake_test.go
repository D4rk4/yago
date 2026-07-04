package rwi

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: yagomodel.WordHash("self"), NetworkName: "freeworld"}
}

type rwiPorts struct {
	Index    PostingIndex
	Receiver PostingReceiver
	Purger   PostingPurger
}

type harness struct {
	vault    *vault.Vault
	urls     urlmeta.URLReceiver
	rwi      rwiPorts
	observer *recordingObserver
}

func openHarness(t *testing.T, quotaBytes int64, batchCap int) harness {
	t.Helper()

	v, err := memvault.Open(quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	directory, _, urlReceiver, err := urlmeta.Open(v)
	if err != nil {
		t.Fatalf("urlmeta.Open: %v", err)
	}
	observer := &recordingObserver{}
	index, receiver, purger, err := Open(
		v,
		directory,
		Config{BatchCap: batchCap, PauseSeconds: 5},
		observer,
	)
	if err != nil {
		t.Fatalf("rwi.Open: %v", err)
	}

	return harness{
		vault:    v,
		urls:     urlReceiver,
		rwi:      rwiPorts{Index: index, Receiver: receiver, Purger: purger},
		observer: observer,
	}
}

func posting(word, urlSeed string) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: yagomodel.WordHash(word),
		Properties: map[string]string{
			yagomodel.ColURLHash:        yagomodel.WordHash(urlSeed).String(),
			yagomodel.ColLocalLinkCount: "1",
			yagomodel.ColHitCount:       "1",
			yagomodel.ColWordDistance:   "0",
		},
	}
}

func urlRow(seed string) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{
		Properties: map[string]string{yagomodel.URLMetaHash: yagomodel.WordHash(seed).String()},
	}
}

func referencedHash(t *testing.T, entry yagomodel.RWIPosting) yagomodel.Hash {
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
		[]yagomodel.URIMetadataRow{urlRow("u1"), urlRow("u2")},
	); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
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

	rwiCount, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 2 {
		t.Fatalf("RWICount = %d, want 2", rwiCount)
	}
}

func TestIntakeReportsUnknownURL(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)
	entry := posting("w1", "u1")

	receipt, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if len(receipt.UnknownURL) != 1 || receipt.UnknownURL[0] != referencedHash(t, entry) {
		t.Fatalf("UnknownURL = %v, want the referenced hash", receipt.UnknownURL)
	}

	if _, err := h.urls.Receive(ctx, []yagomodel.URIMetadataRow{urlRow("u1")}); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err = h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry})
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

	receipt, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{posting("w1", "u1")})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy {
		t.Fatalf("first receipt = %+v, want stored", receipt)
	}

	receipt, err = h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{posting("w2", "u2")})
	if err != nil {
		t.Fatalf("Intake over capacity: %v", err)
	}
	if !receipt.Busy || receipt.Pause != 5 {
		t.Fatalf("receipt = %+v, want Busy with pause 5", receipt)
	}
}

func TestIntakeBusyOverBatchCap(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 1)

	receipt, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
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
