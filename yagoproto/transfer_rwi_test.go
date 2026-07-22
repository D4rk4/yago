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

func TestTransferRWIRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.TransferRWIRequest{
		NetworkName:        yagoproto.DefaultNetwork,
		NetworkNamePresent: true,
		Iam:                sampleHash(t, "alpha"),
		YouAre:             sampleHash(t, "beta"),
		WordCount:          2,
		EntryCount:         2,
		Indexes: []yagomodel.RWIPosting{
			sampleRWIPosting(t, "alpha", "url-a"),
			sampleRWIPosting(t, "beta", "url-b"),
		},
		Key: "salt",
	}

	got, err := yagoproto.ParseTransferRWIRequest(context.Background(), req.Form())
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
	got yagoproto.TransferRWIRequest,
	want yagoproto.TransferRWIRequest,
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

	resp := yagoproto.TransferRWIResponse{
		ResponseHeader:         yagoproto.ResponseHeader{Version: "1.0", Uptime: 7},
		Result:                 yagoproto.ResultOK,
		Pause:                  1500,
		UnknownURLFieldPresent: true,
		UnknownURL: []yagomodel.Hash{
			sampleHash(t, "url-a"),
			sampleHash(t, "url-b"),
		},
		ErrorURL: []yagomodel.Hash{
			sampleHash(t, "url-c"),
		},
	}

	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseTransferRWIResponse(msg)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestTransferRWIResponseIncludesEmptyURLFields(t *testing.T) {
	t.Parallel()

	resp := yagoproto.TransferRWIResponse{Result: yagoproto.ResultOK}
	msg := resp.Encode()

	if _, ok := msg[yagoproto.FieldUnknownURL]; !ok {
		t.Fatal("missing empty unknownURL field")
	}
	if _, ok := msg[yagoproto.FieldErrorURL]; !ok {
		t.Fatal("missing empty errorURL field")
	}
}

func TestParseTransferRWIResponseRequiresUnknownURLForOK(t *testing.T) {
	t.Parallel()

	missing := yagomodel.Message{yagoproto.FieldResult: yagoproto.ResultOK}
	if _, err := yagoproto.ParseTransferRWIResponse(missing); err == nil {
		t.Fatal("expected missing unknownURL error")
	}

	present := yagomodel.Message{
		yagoproto.FieldResult:     yagoproto.ResultOK,
		yagoproto.FieldUnknownURL: "",
	}
	response, err := yagoproto.ParseTransferRWIResponse(present)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}
	if !response.UnknownURLFieldPresent || len(response.UnknownURL) != 0 {
		t.Fatalf("response = %#v", response)
	}

	rejected, err := yagoproto.ParseTransferRWIResponse(
		yagomodel.Message{yagoproto.FieldResult: string(yagoproto.ResultBusy)},
	)
	if err != nil || rejected.UnknownURLFieldPresent {
		t.Fatalf("rejected response = %#v error=%v", rejected, err)
	}
}

func TestParseTransferRWIRequestSkipsBadEntry(t *testing.T) {
	t.Parallel()

	good := sampleRWIPosting(t, "alpha", "url-a")
	form := url.Values{yagoproto.FieldIndexes: {"not-a-posting-line\n" + good.String()}}
	req, err := yagoproto.ParseTransferRWIRequest(context.Background(), form)
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
	form := url.Values{yagoproto.FieldIndexes: {"\n" + good.String()}}
	req, err := yagoproto.ParseTransferRWIRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1 {
		t.Fatalf("Indexes = %d, want 1", len(req.Indexes))
	}
}

func TestParseTransferRWIRequestHandlesEmptyIndexes(t *testing.T) {
	t.Parallel()

	req, err := yagoproto.ParseTransferRWIRequest(context.Background(), url.Values{})
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

	req, err := yagoproto.ParseTransferRWIRequest(
		context.Background(),
		yagoproto.TransferRWIRequest{
			NetworkName:        yagoproto.DefaultNetwork,
			NetworkNamePresent: true,
			WordCount:          0,
			EntryCount:         0,
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
		{yagoproto.FieldWordCount: {"many"}},
		{yagoproto.FieldEntryCount: {"many"}},
		{yagoproto.FieldIam: {"short"}},
		{
			yagoproto.FieldIam:    {sampleHash(t, "alpha").String()},
			yagoproto.FieldYouAre: {"short"},
		},
	}
	for _, form := range cases {
		if _, err := yagoproto.ParseTransferRWIRequest(context.Background(), form); err == nil {
			t.Fatalf("ParseTransferRWIRequest(%v) should fail", form)
		}
	}
}

func TestParseTransferRWIRequestMarksExcessEntriesWithoutAcceptingTheTail(t *testing.T) {
	posting := sampleRWIPosting(t, "alpha", "url-a").String()
	lines := make([]string, 1001)
	for i := range lines {
		lines[i] = posting
	}
	form := url.Values{yagoproto.FieldIndexes: {strings.Join(lines, "\n")}}
	req, err := yagoproto.ParseTransferRWIRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	if len(req.Indexes) != 1000 {
		t.Fatalf("Indexes = %d, want 1000", len(req.Indexes))
	}
	if !req.ExceedsEntryLimit() {
		t.Fatal("oversized payload was not marked")
	}
}

func TestTransferRWIRequestMarksExcessDeclaredEntries(t *testing.T) {
	req, err := yagoproto.ParseTransferRWIRequest(context.Background(), url.Values{
		yagoproto.FieldEntryCount: {"1001"},
		yagoproto.FieldIndexes:    {sampleRWIPosting(t, "alpha", "url-a").String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !req.ExceedsEntryLimit() {
		t.Fatal("oversized declared count was not marked")
	}
}

func TestParseTransferRWIResponseRejectsBadPause(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldPause: "soon"}
	if _, err := yagoproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric pause")
	}
}

func TestParseTransferRWIResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yagomodel.Message{
		{yagoproto.FieldUptime: "soon"},
		{
			yagoproto.FieldResult:     yagoproto.ResultOK,
			yagoproto.FieldUnknownURL: "short",
		},
		{
			yagoproto.FieldResult:     yagoproto.ResultOK,
			yagoproto.FieldUnknownURL: "",
			yagoproto.FieldErrorURL:   "short",
		},
	}
	for _, msg := range cases {
		if _, err := yagoproto.ParseTransferRWIResponse(msg); err == nil {
			t.Fatalf("ParseTransferRWIResponse(%v) should fail", msg)
		}
	}
}

func TestParseTransferRWIResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldResult: "later"}
	if _, err := yagoproto.ParseTransferRWIResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferRWI result")
	}
}
