package yacyproto_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestTransferURLRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.TransferURLRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         sampleHash(t, "alpha"),
		YouAre:      sampleHash(t, "beta"),
		URLCount:    2,
		URLs: []yacymodel.URIMetadataRow{
			sampleURLRow(t, "url-a"),
			sampleURLRow(t, "url-b"),
		},
	}

	got, err := yacyproto.ParseTransferURLRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestTransferURLResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yacyproto.TransferURLResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 9},
		Result:         yacyproto.ResultErrorNotGranted,
		Double:         3,
		ErrorURL: []yacymodel.Hash{
			sampleHash(t, "url-a"),
		},
	}

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseTransferURLResponse(msg)
	if err != nil {
		t.Fatalf("ParseTransferURLResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseTransferURLResponseAcceptsEmptyResult(t *testing.T) {
	t.Parallel()

	got, err := yacyproto.ParseTransferURLResponse(yacymodel.Message{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != "" {
		t.Fatalf("Result = %q, want empty", got.Result)
	}
}

func TestParseTransferURLRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldYouAre: {"!!"}}
	if _, err := yacyproto.ParseTransferURLRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseTransferURLRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yacyproto.FieldURLCount: {"many"}},
		{yacyproto.FieldIam: {"short"}},
	}
	for _, form := range cases {
		if _, err := yacyproto.ParseTransferURLRequest(context.Background(), form); err == nil {
			t.Fatalf("ParseTransferURLRequest(%v) should fail", form)
		}
	}
}

func TestParseTransferURLRequestSkipsMissingDeclaredURL(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yacyproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yacyproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yacyproto.FieldURLCount: {"2"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
	}
	req, err := yacyproto.ParseTransferURLRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}
	if len(req.URLs) != 1 {
		t.Fatalf("URLs = %d, want 1 (missing url1 skipped)", len(req.URLs))
	}
}

func TestParseTransferURLRequestSkipsBadDeclaredURL(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yacyproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yacyproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yacyproto.FieldURLCount: {"2"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
		"url1":                  {"bad"},
	}
	req, err := yacyproto.ParseTransferURLRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}
	if len(req.URLs) != 1 {
		t.Fatalf("URLs = %d, want 1 (bad url1 skipped)", len(req.URLs))
	}
}

func TestParseTransferURLResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yacymodel.Message{
		{yacyproto.FieldUptime: "soon"},
		{yacyproto.FieldDouble: "many"},
		{yacyproto.FieldErrorURL: "short"},
	}
	for _, msg := range cases {
		if _, err := yacyproto.ParseTransferURLResponse(msg); err == nil {
			t.Fatalf("ParseTransferURLResponse(%v) should fail", msg)
		}
	}
}

func TestParseTransferURLResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldResult: "later"}
	if _, err := yacyproto.ParseTransferURLResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferURL result")
	}
}
