package crawling

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type mountedReceiptStatus struct{}

func (mountedReceiptStatus) Version(context.Context) string {
	return "test-version"
}

func (mountedReceiptStatus) Uptime(context.Context) int {
	return 12
}

func TestCrawlReceiptRejectsCrawl(t *testing.T) {
	req := yacyproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yacymodel.WordHash("caller"),
		YouAre:      yacymodel.WordHash("self"),
	}

	resp, err := disabledCrawlReceiptEndpoint{}.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Delay != 0 {
		t.Fatalf("Delay = %d, want 0 (no crawl accepted)", resp.Delay)
	}
}

func TestMountCrawlReceiptServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedReceiptStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	MountCrawlReceipt(router)
	form := yacyproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yacymodel.WordHash("caller"),
		YouAre:      yacymodel.WordHash("self"),
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathCrawlReceipt,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yacyproto.ParseCrawlReceiptResponse(msg)
	if err != nil {
		t.Fatalf("parse crawl receipt response: %v", err)
	}
	if resp.Delay != 0 {
		t.Fatalf("Delay = %d, want 0", resp.Delay)
	}
}
