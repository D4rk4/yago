package rwi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type fixedStatus struct{}

func (fixedStatus) Version(context.Context) string { return "1.0" }

func (fixedStatus) Uptime(context.Context) int { return 7 }

func localIdentity() httpguard.LocalPeer {
	return httpguard.LocalPeer{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

type harness struct {
	vault *boltvault.Vault
	urls  urlmeta.Module
	rwi   rwi.Module
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

	guard := httpguard.NewRequestGuard(localIdentity(), httpguard.DefaultMaxBodyBytes, time.Second)
	respond := httpguard.NewWireResponder(fixedStatus{})
	urls, err := urlmeta.New(vault, guard, respond)
	if err != nil {
		t.Fatalf("urlmeta.New: %v", err)
	}
	module, err := rwi.New(
		vault,
		guard,
		respond,
		urls.Directory,
		rwi.Config{BatchCap: batchCap, PauseSeconds: 5},
	)
	if err != nil {
		t.Fatalf("rwi.New: %v", err)
	}

	return harness{vault: vault, urls: urls, rwi: module}
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

	if _, err := h.urls.Intake(
		ctx,
		[]yacymodel.URIMetadataRow{urlRow("u1"), urlRow("u2")},
	); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{
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

	receipt, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{entry})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if len(receipt.UnknownURL) != 1 || receipt.UnknownURL[0] != referencedHash(t, entry) {
		t.Fatalf("UnknownURL = %v, want the referenced hash", receipt.UnknownURL)
	}

	if _, err := h.urls.Intake(ctx, []yacymodel.URIMetadataRow{urlRow("u1")}); err != nil {
		t.Fatalf("urls.Intake: %v", err)
	}

	receipt, err = h.rwi.Intake(ctx, []yacymodel.RWIPosting{entry})
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

	receipt, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{posting("w1", "u1")})
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

	receipt, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{
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

func TestScanWordVisitsMatchingPostings(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u3"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	word := yacymodel.WordHash("w1")
	var visited []yacymodel.RWIPosting
	err := h.rwi.Index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
		visited = append(visited, entry)

		return true, nil
	})
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if len(visited) != 2 {
		t.Fatalf("visited %d postings, want 2", len(visited))
	}
	for _, entry := range visited {
		if entry.WordHash != word {
			t.Fatalf("entry word hash = %q, want %q", entry.WordHash, word)
		}
	}
}

func TestScanWordStopsWhenVisitorStops(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	visited := 0
	err := h.rwi.Index.ScanWord(
		ctx,
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			visited++

			return false, nil
		},
	)
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited %d postings, want 1 before stop", visited)
	}
}

func TestEndpointReportsBusy(t *testing.T) {
	h := openHarness(t, 1, 100)

	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathTransferRWI,
		nil,
	)
	httpReq.PostForm = req.Form()

	h.rwi.TransferRWI.ServeHTTP(rec, httpReq)

	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseTransferRWIResponse(message)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}
	if resp.Result != yacyproto.ResultBusy {
		t.Fatalf("Result = %q, want busy", resp.Result)
	}
	if resp.Pause != 5 {
		t.Fatalf("Pause = %d, want 5", resp.Pause)
	}
}

func TestPurgeReferencesDropsPostingsAndReferences(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Intake(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u1"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	target := referencedHash(t, posting("w1", "u1"))
	var result rwi.PurgeResult
	err := h.vault.Update(ctx, func(tx *boltvault.Txn) error {
		var purgeErr error
		result, purgeErr = h.rwi.Directory.PurgeReferences(tx, []yacymodel.Hash{target})
		if purgeErr != nil {
			return fmt.Errorf("purge references: %w", purgeErr)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("PurgeReferences: %v", err)
	}
	if result.PostingsDeleted != 2 || result.ReferencesDeleted != 1 {
		t.Fatalf("result = %+v, want 2 postings and 1 reference", result)
	}

	rwiCount, err := h.rwi.Directory.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 1 {
		t.Fatalf("RWICount = %d, want 1", rwiCount)
	}
	refCount, err := h.rwi.Directory.ReferencedURLCount(ctx)
	if err != nil {
		t.Fatalf("ReferencedURLCount: %v", err)
	}
	if refCount != 1 {
		t.Fatalf("ReferencedURLCount = %d, want 1", refCount)
	}
}

func TestEndpointStoresAndAnswers(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(ctx, http.MethodPost, yacyproto.PathTransferRWI, nil)
	httpReq.PostForm = req.Form()

	h.rwi.TransferRWI.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseTransferRWIResponse(message)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}

	rwiCount, err := h.rwi.Directory.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 1 {
		t.Fatalf("RWICount = %d, want 1", rwiCount)
	}
}

func TestEndpointRejectsWrongNetwork(t *testing.T) {
	h := openHarness(t, 0, 100)

	req := yacyproto.TransferRWIRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathTransferRWI,
		nil,
	)
	httpReq.PostForm = req.Form()

	h.rwi.TransferRWI.ServeHTTP(rec, httpReq)

	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, _ := yacyproto.ParseTransferRWIResponse(message)
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
