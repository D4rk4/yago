package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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

	got, err := yacyproto.ParseCrawlReceiptRequest(req.Form())
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

	got, err := yacyproto.ParseCrawlReceiptResponse(resp.Encode())
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
	if _, err := yacyproto.ParseCrawlReceiptRequest(form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseCrawlReceiptResponseRejectsBadDelay(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldDelay: "later"}
	if _, err := yacyproto.ParseCrawlReceiptResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric delay")
	}
}
