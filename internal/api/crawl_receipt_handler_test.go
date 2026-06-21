package api

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestCrawlReceiptHandlerRejects(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.CrawlReceiptRequest{Iam: testHash(t, "caller"), YouAre: h.ident.Hash}
	rec := h.do(t, http.MethodPost, yacyproto.PathCrawlReceipt, req.Form())

	resp, err := yacyproto.ParseCrawlReceiptResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", resp.Version)
	}
	if resp.Delay != 0 {
		t.Errorf("Delay = %d, want 0 (no crawl accepted)", resp.Delay)
	}
}

func TestCrawlReceiptHandlerWrongMethod(t *testing.T) {
	h := newTestHarness(t)
	rec := h.do(t, http.MethodGet, yacyproto.PathCrawlReceipt, nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
