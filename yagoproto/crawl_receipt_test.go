package yagoproto_test

import (
	"context"
	"net/url"
	"reflect"
	"strings"
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

func TestCrawlReceiptResultVocabulary(t *testing.T) {
	t.Parallel()

	for _, result := range []string{
		yagoproto.CrawlReceiptResultUnavailable,
		yagoproto.CrawlReceiptResultException,
		yagoproto.CrawlReceiptResultRobot,
		yagoproto.CrawlReceiptResultRejected,
		yagoproto.CrawlReceiptResultDequeue,
		yagoproto.CrawlReceiptResultFill,
		yagoproto.CrawlReceiptResultUpdate,
		yagoproto.CrawlReceiptResultKnown,
		yagoproto.CrawlReceiptResultStale,
	} {
		if !yagoproto.ValidCrawlReceiptResult(result) {
			t.Errorf("result %q rejected", result)
		}
	}
	if yagoproto.ValidCrawlReceiptResult("ok") {
		t.Fatal("unknown result accepted")
	}
}

func TestParseCrawlReceiptRequestBoundsWireFields(t *testing.T) {
	t.Parallel()

	for field, maximum := range map[string]int{
		yagoproto.FieldResult:    yagoproto.MaximumCrawlReceiptResultBytes,
		yagoproto.FieldReason:    yagoproto.MaximumCrawlReceiptReasonBytes,
		yagoproto.FieldLURLEntry: yagoproto.MaximumCrawlReceiptMetadataBytes,
	} {
		form := url.Values{field: {strings.Repeat("x", maximum+1)}}
		if _, err := yagoproto.ParseCrawlReceiptRequest(t.Context(), form); err == nil {
			t.Errorf("oversized %s accepted", field)
		}
	}
}
