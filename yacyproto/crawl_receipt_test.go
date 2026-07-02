package yacyproto_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestCrawlReceiptRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.CrawlReceiptRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         sampleHash(t, "alpha"),
		YouAre:      sampleHash(t, "beta"),
		Result:      "fill",
		Reason:      "ok",
		LURLEntry:   "encoded-entry",
	}

	got, err := yacyproto.ParseCrawlReceiptRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseCrawlReceiptRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestCrawlReceiptResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yacyproto.CrawlReceiptResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 5},
		Delay:          60,
	}

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseCrawlReceiptResponse(msg)
	if err != nil {
		t.Fatalf("ParseCrawlReceiptResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseCrawlReceiptRequestRejectsBadIam(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldIam: {"x"}}
	if _, err := yacyproto.ParseCrawlReceiptRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseCrawlReceiptRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yacyproto.FieldIam:    {sampleHash(t, "alpha").String()},
		yacyproto.FieldYouAre: {"x"},
	}
	if _, err := yacyproto.ParseCrawlReceiptRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseCrawlReceiptResponseRejectsBadHeader(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldUptime: "later"}
	if _, err := yacyproto.ParseCrawlReceiptResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
}

func TestParseCrawlReceiptResponseRejectsBadDelay(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldDelay: "later"}
	if _, err := yacyproto.ParseCrawlReceiptResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric delay")
	}
}
