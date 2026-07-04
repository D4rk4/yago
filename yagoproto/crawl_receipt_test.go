package yagoproto_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestCrawlReceiptRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.CrawlReceiptRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Iam:         sampleHash(t, "alpha"),
		YouAre:      sampleHash(t, "beta"),
		Result:      "fill",
		Reason:      "ok",
		LURLEntry:   "encoded-entry",
	}

	got, err := yagoproto.ParseCrawlReceiptRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseCrawlReceiptRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestCrawlReceiptResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yagoproto.CrawlReceiptResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.0", Uptime: 5},
		Delay:          60,
	}

	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseCrawlReceiptResponse(msg)
	if err != nil {
		t.Fatalf("ParseCrawlReceiptResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseCrawlReceiptRequestIgnoresMalformedHashes(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldIam:    {"x"},
		yagoproto.FieldYouAre: {"x"},
	}
	got, err := yagoproto.ParseCrawlReceiptRequest(context.Background(), form)
	if err != nil {
		t.Fatal(err)
	}

	if got.Iam != "" || got.YouAre != "" {
		t.Fatalf("hashes = %q/%q, want empty", got.Iam, got.YouAre)
	}
}

func TestCrawlReceiptResponseOmitsEmptyDelay(t *testing.T) {
	t.Parallel()

	msg := yagoproto.CrawlReceiptResponse{}.Encode()
	if _, ok := msg[yagoproto.FieldDelay]; ok {
		t.Fatalf("delay encoded for empty response: %v", msg)
	}
}

func TestParseCrawlReceiptResponseRejectsBadHeader(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldUptime: "later"}
	if _, err := yagoproto.ParseCrawlReceiptResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
}

func TestParseCrawlReceiptResponseRejectsBadDelay(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldDelay: "later"}
	if _, err := yagoproto.ParseCrawlReceiptResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric delay")
	}
}
