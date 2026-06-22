package urlmeta_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type fixedStatus struct{}

func (fixedStatus) Version(context.Context) string { return "1.0" }

func (fixedStatus) Uptime(context.Context) int { return 7 }

func localIdentity() httpguard.LocalPeer {
	return httpguard.LocalPeer{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

func openModule(t *testing.T, quotaBytes int64) urlmeta.Module {
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

	guard := httpguard.NewRequestGuard(localIdentity(), httpguard.DefaultMaxBodyBytes, time.Second)
	module, err := urlmeta.New(vault, guard, httpguard.NewWireResponder(fixedStatus{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return module
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

func TestDirectoryPersistsAndReportsExisting(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	first := urlRow(t, "a")
	second := urlRow(t, "b")

	receipt, err := module.Intake(ctx, []yacymodel.URIMetadataRow{first, second})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy || receipt.Double != 0 || len(receipt.ErrorURL) != 0 {
		t.Fatalf("first receipt = %+v, want empty", receipt)
	}

	receipt, err = module.Intake(ctx, []yacymodel.URIMetadataRow{first})
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

func TestDirectoryDurabilityAndLookup(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	row := urlRow(t, "a")
	hash := rowHash(t, row)

	if _, err := module.Intake(ctx, []yacymodel.URIMetadataRow{row}); err != nil {
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

	receipt, err := module.Intake(ctx, []yacymodel.URIMetadataRow{urlRow(t, "a")})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want Busy", receipt)
	}
}

func TestEndpointStoresAndAnswers(t *testing.T) {
	module := openModule(t, 0)

	req := yacyproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yacymodel.URIMetadataRow{urlRow(t, "a")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		nil,
	)
	httpReq.PostForm = req.Form()

	module.Endpoint.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseTransferURLResponse(message)
	if err != nil {
		t.Fatalf("ParseTransferURLResponse: %v", err)
	}
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
	if resp.Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", resp.Version)
	}
}

func TestEndpointRejectsWrongNetwork(t *testing.T) {
	module := openModule(t, 0)

	req := yacyproto.TransferURLRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		nil,
	)
	httpReq.PostForm = req.Form()

	module.Endpoint.ServeHTTP(rec, httpReq)

	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, _ := yacyproto.ParseTransferURLResponse(message)
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
