package yagoproto_test

import (
	"context"
	"errors"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestTransferURLRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.TransferURLRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Iam:         sampleHash(t, "alpha"),
		YouAre:      sampleHash(t, "beta"),
		URLCount:    2,
		URLs: []yagomodel.URIMetadataRow{
			sampleURLRow(t, "url-a"),
			sampleURLRow(t, "url-b"),
		},
	}

	got, err := yagoproto.ParseTransferURLRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestTransferURLResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yagoproto.TransferURLResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.0", Uptime: 9},
		Result:         yagoproto.ResultErrorNotGranted,
		Double:         3,
		ErrorURL: []yagomodel.Hash{
			sampleHash(t, "url-a"),
		},
	}

	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseTransferURLResponse(msg)
	if err != nil {
		t.Fatalf("ParseTransferURLResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestTransferURLResponseEncodeOmitsEmptyTransferFields(t *testing.T) {
	t.Parallel()

	if got := (yagoproto.TransferURLResponse{}).Encode(); len(got) != 0 {
		t.Fatalf("Encode = %+v, want empty message", got)
	}
}

func TestParseTransferURLResponseAcceptsEmptyResult(t *testing.T) {
	t.Parallel()

	got, err := yagoproto.ParseTransferURLResponse(yagomodel.Message{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != "" {
		t.Fatalf("Result = %q, want empty", got.Result)
	}
}

func TestParseTransferURLRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{yagoproto.FieldYouAre: {"!!"}}
	if _, err := yagoproto.ParseTransferURLRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseTransferURLRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yagoproto.FieldURLCount: {"many"}},
		{yagoproto.FieldIam: {"short"}},
	}
	for _, form := range cases {
		if _, err := yagoproto.ParseTransferURLRequest(context.Background(), form); err == nil {
			t.Fatalf("ParseTransferURLRequest(%v) should fail", form)
		}
	}
}

func TestParseTransferURLRequestSkipsMissingDeclaredURL(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yagoproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yagoproto.FieldURLCount: {"2"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
	}
	req, err := yagoproto.ParseTransferURLRequest(context.Background(), form)
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
		yagoproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yagoproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yagoproto.FieldURLCount: {"2"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
		"url1":                  {"bad"},
	}
	req, err := yagoproto.ParseTransferURLRequest(context.Background(), form)
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}
	if len(req.URLs) != 1 {
		t.Fatalf("URLs = %d, want 1 (bad url1 skipped)", len(req.URLs))
	}
}

func TestParseTransferURLRequestBoundsDeclaredRows(t *testing.T) {
	iam := sampleHash(t, "alpha").String()
	youAre := sampleHash(t, "beta").String()
	row := sampleURLRow(t, "url-a").String()
	form := url.Values{
		yagoproto.FieldIam:      {iam},
		yagoproto.FieldYouAre:   {youAre},
		yagoproto.FieldURLCount: {strconv.Itoa(yagoproto.MaximumTransferEntries)},
	}
	for index := range yagoproto.MaximumTransferEntries {
		form.Set("url"+strconv.Itoa(index), row)
	}
	request, err := yagoproto.ParseTransferURLRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.URLs) != yagoproto.MaximumTransferEntries {
		t.Fatalf("URLs = %d, want %d", len(request.URLs), yagoproto.MaximumTransferEntries)
	}

	form = url.Values{
		yagoproto.FieldIam:      {iam},
		yagoproto.FieldYouAre:   {youAre},
		yagoproto.FieldURLCount: {strconv.Itoa(math.MaxInt)},
	}
	if _, err := yagoproto.ParseTransferURLRequest(t.Context(), form); !errors.Is(
		err,
		yagoproto.ErrBadField,
	) {
		t.Fatalf("error = %v, want bad field", err)
	}
}

func TestParseTransferURLRequestHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	form := url.Values{
		yagoproto.FieldIam:      {sampleHash(t, "alpha").String()},
		yagoproto.FieldYouAre:   {sampleHash(t, "beta").String()},
		yagoproto.FieldURLCount: {"1"},
		"url0":                  {sampleURLRow(t, "url-a").String()},
	}
	if _, err := yagoproto.ParseTransferURLRequest(ctx, form); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestParseTransferURLResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yagomodel.Message{
		{yagoproto.FieldUptime: "soon"},
		{yagoproto.FieldDouble: "many"},
		{yagoproto.FieldErrorURL: "short"},
	}
	for _, msg := range cases {
		if _, err := yagoproto.ParseTransferURLResponse(msg); err == nil {
			t.Fatalf("ParseTransferURLResponse(%v) should fail", msg)
		}
	}
}

func TestParseTransferURLResponseRejectsUnknownResult(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldResult: "later"}
	if _, err := yagoproto.ParseTransferURLResponse(msg); err == nil {
		t.Fatal("expected error for unknown transferURL result")
	}
}
