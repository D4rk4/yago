package crawling

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type mountedReceiptStatus struct{}

func (mountedReceiptStatus) Version(context.Context) string {
	return "test-version"
}

func (mountedReceiptStatus) Uptime(context.Context) int {
	return 12
}

type localPeer struct{}

func (localPeer) NetworkMatches(network string) bool {
	return network == "freeworld"
}

func (localPeer) Addresses(network string, youare yagomodel.Hash) bool {
	return network == "freeworld" && youare == yagomodel.WordHash("self")
}

func TestCrawlReceiptRejectsCrawl(t *testing.T) {
	req := yagoproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("self"),
	}

	resp, err := disabledCrawlReceiptEndpoint{
		local: localPeer{},
	}.Serve(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Delay != disabledCrawlReceiptRetryDelay {
		t.Fatalf("Delay = %d, want retry delay", resp.Delay)
	}
}

func TestCrawlReceiptDelaysWrongTarget(t *testing.T) {
	req := yagoproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("other"),
	}

	resp, err := disabledCrawlReceiptEndpoint{
		local: localPeer{},
	}.Serve(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Delay != disabledCrawlReceiptRetryDelay {
		t.Fatalf("Delay = %d, want retry delay", resp.Delay)
	}
}

func TestCrawlReceiptIgnoresForeignNetwork(t *testing.T) {
	req := yagoproto.CrawlReceiptRequest{
		NetworkName: "foreign",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("self"),
	}

	resp, err := disabledCrawlReceiptEndpoint{
		local: localPeer{},
	}.Serve(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Delay != 0 {
		t.Fatalf("Delay = %d, want empty response", resp.Delay)
	}
}

func TestMountCrawlReceiptServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedReceiptStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	MountCrawlReceipt(router, localPeer{})
	form := yagoproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("self"),
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yagoproto.PathCrawlReceipt,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yagoproto.ParseCrawlReceiptResponse(msg)
	if err != nil {
		t.Fatalf("parse crawl receipt response: %v", err)
	}
	if resp.Delay != disabledCrawlReceiptRetryDelay {
		t.Fatalf("Delay = %d, want retry delay", resp.Delay)
	}
}

func TestMountCrawlReceiptDelaysMalformedTarget(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedReceiptStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	MountCrawlReceipt(router, localPeer{})
	form := yagoproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("self"),
	}.Form()
	form.Set(yagoproto.FieldIam, "x")
	form.Set(yagoproto.FieldYouAre, "x")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yagoproto.PathCrawlReceipt,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yagoproto.ParseCrawlReceiptResponse(msg)
	if err != nil {
		t.Fatalf("parse crawl receipt response: %v", err)
	}
	if resp.Delay != disabledCrawlReceiptRetryDelay {
		t.Fatalf("Delay = %d, want retry delay", resp.Delay)
	}
}

func TestMountCrawlReceiptOmitsDelayForForeignNetwork(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedReceiptStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	MountCrawlReceipt(router, localPeer{})
	form := yagoproto.CrawlReceiptRequest{
		NetworkName: "foreign",
		Iam:         yagomodel.WordHash("caller"),
		YouAre:      yagomodel.WordHash("self"),
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yagoproto.PathCrawlReceipt,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	if _, ok := msg[yagoproto.FieldDelay]; ok {
		t.Fatalf("delay encoded for foreign network: %v", msg)
	}
}
