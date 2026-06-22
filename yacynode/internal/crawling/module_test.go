package crawling_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawling"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type fixedStatus struct{}

func (fixedStatus) Snapshot(context.Context) crawling.StatusSnapshot {
	return crawling.StatusSnapshot{Version: "1.0", Uptime: 7}
}

func localIdentity() httpguard.LocalPeer {
	return httpguard.LocalPeer{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

func newModule() crawling.Module {
	guard := httpguard.NewRequestGuard(localIdentity(), httpguard.DefaultMaxBodyBytes, time.Second)

	return crawling.New(guard, fixedStatus{})
}

func TestEndpointRejectsCrawl(t *testing.T) {
	req := yacyproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yacymodel.WordHash("caller"),
		YouAre:      localIdentity().Hash,
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathCrawlReceipt,
		nil,
	)
	httpReq.PostForm = req.Form()

	newModule().Endpoint.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseCrawlReceiptResponse(message)
	if err != nil {
		t.Fatalf("ParseCrawlReceiptResponse: %v", err)
	}
	if resp.Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", resp.Version)
	}
	if resp.Delay != 0 {
		t.Fatalf("Delay = %d, want 0 (no crawl accepted)", resp.Delay)
	}
}

func TestEndpointRejectsWrongMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yacyproto.PathCrawlReceipt,
		nil,
	)

	newModule().Endpoint.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
