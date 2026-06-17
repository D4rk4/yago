package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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

	got, err := yacyproto.ParseTransferURLRequest(req.Form())
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

	got, err := yacyproto.ParseTransferURLResponse(resp.Encode())
	if err != nil {
		t.Fatalf("ParseTransferURLResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseTransferURLRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldYouAre: {"!!"}}
	if _, err := yacyproto.ParseTransferURLRequest(form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseTransferURLRequestRejectsMissingDeclaredURL(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yacyproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yacyproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yacyproto.FieldURLCount: {"2"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
	}
	if _, err := yacyproto.ParseTransferURLRequest(form); err == nil {
		t.Fatal("expected error for missing url1")
	}
}

func TestParseTransferURLResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldResult: "later"}
	if _, err := yacyproto.ParseTransferURLResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferURL result")
	}
}
