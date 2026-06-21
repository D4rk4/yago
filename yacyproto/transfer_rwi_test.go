package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestTransferRWIRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.TransferRWIRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         sampleHash(t, "alpha"),
		YouAre:      sampleHash(t, "beta"),
		WordCount:   2,
		EntryCount:  2,
		Indexes: []yacymodel.RWIPosting{
			sampleRWIPosting(t, "alpha", "url-a"),
			sampleRWIPosting(t, "beta", "url-b"),
		},
		Key: "salt",
	}

	got, err := yacyproto.ParseTransferRWIRequest(req.Form())
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestTransferRWIResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yacyproto.TransferRWIResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 7},
		Result:         yacyproto.ResultOK,
		Pause:          1500,
		UnknownURL: []yacymodel.Hash{
			sampleHash(t, "url-a"),
			sampleHash(t, "url-b"),
		},
		ErrorURL: []yacymodel.Hash{
			sampleHash(t, "url-c"),
		},
	}

	got, err := yacyproto.ParseTransferRWIResponse(resp.Encode())
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestTransferRWIResponseIncludesEmptyURLFields(t *testing.T) {
	t.Parallel()

	resp := yacyproto.TransferRWIResponse{Result: yacyproto.ResultOK}
	msg := resp.Encode()

	if _, ok := msg[yacyproto.FieldUnknownURL]; !ok {
		t.Fatal("missing empty unknownURL field")
	}
	if _, ok := msg[yacyproto.FieldErrorURL]; !ok {
		t.Fatal("missing empty errorURL field")
	}
}

func TestParseTransferRWIRequestSkipsBadEntry(t *testing.T) {
	t.Parallel()

	good := sampleRWIPosting(t, "alpha", "url-a")
	form := url.Values{yacyproto.FieldIndexes: {"not-a-posting-line\n" + good.String()}}
	req, err := yacyproto.ParseTransferRWIRequest(form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1 {
		t.Fatalf("Indexes = %d, want 1 (malformed line skipped)", len(req.Indexes))
	}
}

func TestParseTransferRWIResponseRejectsBadPause(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldPause: "soon"}
	if _, err := yacyproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric pause")
	}
}

func TestParseTransferRWIResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldResult: "later"}
	if _, err := yacyproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferRWI result")
	}
}
