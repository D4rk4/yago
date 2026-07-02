package yacyproto_test

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
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

	got, err := yacyproto.ParseTransferRWIRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}

	assertTransferRWIRequest(t, got, req)
	if got.MissingWordCountField() ||
		got.MissingEntryCountField() ||
		got.MissingIndexesField() {
		t.Fatalf("required fields reported missing: %#v", got)
	}
}

func assertTransferRWIRequest(
	t *testing.T,
	got yacyproto.TransferRWIRequest,
	want yacyproto.TransferRWIRequest,
) {
	t.Helper()

	if got.NetworkName != want.NetworkName ||
		got.Iam != want.Iam ||
		got.YouAre != want.YouAre ||
		got.WordCount != want.WordCount ||
		got.EntryCount != want.EntryCount ||
		got.Key != want.Key {
		t.Fatalf("request fields:\n got %#v\nwant %#v", got, want)
	}
	if len(got.Indexes) != len(want.Indexes) {
		t.Fatalf("indexes = %d, want %d", len(got.Indexes), len(want.Indexes))
	}
	for i := range got.Indexes {
		if !reflect.DeepEqual(got.Indexes[i], want.Indexes[i]) {
			t.Fatalf("index %d:\n got %#v\nwant %#v", i, got.Indexes[i], want.Indexes[i])
		}
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

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseTransferRWIResponse(msg)
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
	req, err := yacyproto.ParseTransferRWIRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1 {
		t.Fatalf("Indexes = %d, want 1 (malformed line skipped)", len(req.Indexes))
	}
}

func TestParseTransferRWIRequestHandlesEmptyIndexLines(t *testing.T) {
	t.Parallel()

	good := sampleRWIPosting(t, "alpha", "url-a")
	form := url.Values{yacyproto.FieldIndexes: {"\n" + good.String()}}
	req, err := yacyproto.ParseTransferRWIRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1 {
		t.Fatalf("Indexes = %d, want 1", len(req.Indexes))
	}
}

func TestParseTransferRWIRequestHandlesEmptyIndexes(t *testing.T) {
	t.Parallel()

	req, err := yacyproto.ParseTransferRWIRequest(context.Background(), url.Values{})
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if req.Indexes != nil {
		t.Fatalf("Indexes = %+v, want nil", req.Indexes)
	}
	if !req.MissingWordCountField() ||
		!req.MissingEntryCountField() ||
		!req.MissingIndexesField() {
		t.Fatalf("required fields not reported missing: %#v", req)
	}
}

func TestTransferRWIRequestFormIncludesEmptyIndexesField(t *testing.T) {
	t.Parallel()

	req, err := yacyproto.ParseTransferRWIRequest(
		context.Background(),
		yacyproto.TransferRWIRequest{
			NetworkName: yacyproto.DefaultNetwork,
			WordCount:   0,
			EntryCount:  0,
		}.Form(),
	)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if req.MissingWordCountField() ||
		req.MissingEntryCountField() ||
		req.MissingIndexesField() {
		t.Fatalf("form fields reported missing: %#v", req)
	}
}

func TestParseTransferRWIRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yacyproto.FieldWordCount: {"many"}},
		{yacyproto.FieldEntryCount: {"many"}},
		{yacyproto.FieldIam: {"short"}},
		{
			yacyproto.FieldIam:    {sampleHash(t, "alpha").String()},
			yacyproto.FieldYouAre: {"short"},
		},
	}
	for _, form := range cases {
		if _, err := yacyproto.ParseTransferRWIRequest(context.Background(), form); err == nil {
			t.Fatalf("ParseTransferRWIRequest(%v) should fail", form)
		}
	}
}

func TestParseTransferRWIRequestLimitsEntries(t *testing.T) {
	posting := sampleRWIPosting(t, "alpha", "url-a").String()
	lines := make([]string, 1001)
	for i := range lines {
		lines[i] = posting
	}
	form := url.Values{yacyproto.FieldIndexes: {strings.Join(lines, "\n")}}
	req, err := yacyproto.ParseTransferRWIRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1000 {
		t.Fatalf("Indexes = %d, want 1000", len(req.Indexes))
	}
}

func TestParseTransferRWIResponseRejectsBadPause(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldPause: "soon"}
	if _, err := yacyproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric pause")
	}
}

func TestParseTransferRWIResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yacymodel.Message{
		{yacyproto.FieldUptime: "soon"},
		{yacyproto.FieldUnknownURL: "short"},
		{yacyproto.FieldErrorURL: "short"},
	}
	for _, msg := range cases {
		if _, err := yacyproto.ParseTransferRWIResponse(msg); err == nil {
			t.Fatalf("ParseTransferRWIResponse(%v) should fail", msg)
		}
	}
}

func TestParseTransferRWIResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldResult: "later"}
	if _, err := yacyproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferRWI result")
	}
}
