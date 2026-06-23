package urlmeta

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
)

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

type urlPorts struct {
	Directory URLDirectory
	Evictor   URLEvictor
	Receiver  URLReceiver
}

func openModule(t *testing.T, quotaBytes int64) urlPorts {
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

	directory, evictor, receiver, err := Open(vault)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return urlPorts{Directory: directory, Evictor: evictor, Receiver: receiver}
}

func urlRow(t *testing.T, seed string) yacymodel.URIMetadataRow {
	t.Helper()

	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{yacymodel.URLMetaHash: yacymodel.WordHash(seed).String()},
	}
	roundTrip, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("row does not round-trip: %v", err)
	}

	return roundTrip
}

func rowHash(t *testing.T, row yacymodel.URIMetadataRow) yacymodel.Hash {
	t.Helper()

	hash, err := row.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}

	return hash.Hash()
}

func TestIntakePersistsAndReportsExisting(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	first := urlRow(t, "a")
	second := urlRow(t, "b")

	receipt, err := module.Receiver.Receive(ctx, []yacymodel.URIMetadataRow{first, second})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy || receipt.Double != 0 || len(receipt.ErrorURL) != 0 {
		t.Fatalf("first receipt = %+v, want empty", receipt)
	}

	receipt, err = module.Receiver.Receive(ctx, []yacymodel.URIMetadataRow{first})
	if err != nil {
		t.Fatalf("Intake duplicate: %v", err)
	}
	if receipt.Double != 1 {
		t.Fatalf("duplicate Double = %d, want 1", receipt.Double)
	}

	count, err := module.Directory.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Fatalf("Count = %d, want 2", count)
	}
}

func TestIntakeDurabilityAndLookup(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	row := urlRow(t, "a")
	hash := rowHash(t, row)

	if _, err := module.Receiver.Receive(ctx, []yacymodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	rows, err := module.Directory.RowsByHash(ctx, []yacymodel.Hash{hash})
	if err != nil {
		t.Fatalf("RowsByHash: %v", err)
	}
	if len(rows) != 1 || rowHash(t, rows[0]) != hash {
		t.Fatalf("RowsByHash = %v, want one matching row", rows)
	}

	missing, err := module.Directory.MissingURLs(ctx, []yacymodel.Hash{
		hash,
		yacymodel.WordHash("absent"),
		yacymodel.WordHash("absent"),
	})
	if err != nil {
		t.Fatalf("MissingURLs: %v", err)
	}
	if len(missing) != 1 || missing[0] != yacymodel.WordHash("absent") {
		t.Fatalf("MissingURLs = %v, want one absent hash", missing)
	}
}

func TestIntakeBusyAtCapacity(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 1)

	receipt, err := module.Receiver.Receive(ctx, []yacymodel.URIMetadataRow{urlRow(t, "a")})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want Busy", receipt)
	}
}
