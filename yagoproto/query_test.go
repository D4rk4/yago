package yagoproto_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestQueryRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.QueryRequest{
		NetworkName: yagoproto.DefaultNetwork,
		YouAre:      sampleHash(t, "alpha"),
		Iam:         sampleHash(t, "beta"),
		Object:      yagoproto.ObjectRWIURLCount,
		Env:         sampleHash(t, "alpha").String(),
	}

	got, err := yagoproto.ParseQueryRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseQueryRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestQueryRequestAcceptsMissingIam(t *testing.T) {
	t.Parallel()

	req := yagoproto.QueryRequest{
		NetworkName: yagoproto.DefaultNetwork,
		YouAre:      sampleHash(t, "alpha"),
		Object:      yagoproto.ObjectRWICount,
	}

	got, err := yagoproto.ParseQueryRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseQueryRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestQueryResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yagoproto.QueryResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.0", Uptime: 3},
		Response:       yagoproto.QueryResponseRejected,
		MyTime:         "20260617120002",
		Magic:          "deadbeef",
	}

	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		t.Fatalf("ParseQueryResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseQueryRequestRejectsBadIam(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldObject: {string(yagoproto.ObjectRWICount)},
		yagoproto.FieldYouAre: {sampleHash(t, "alpha").String()},
		yagoproto.FieldIam:    {"nope"},
	}
	if _, err := yagoproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseQueryRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldObject: {string(yagoproto.ObjectRWICount)},
		yagoproto.FieldYouAre: {"nope"},
	}
	if _, err := yagoproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseQueryRequestRejectsUnknownObject(t *testing.T) {
	t.Parallel()

	form := url.Values{yagoproto.FieldObject: {"whatever"}}
	if _, err := yagoproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for unknown query object")
	}
}

func TestParseQueryResponseRejectsBadResponse(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldResponse: "many"}
	if _, err := yagoproto.ParseQueryResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric response")
	}
}

func TestParseQueryResponseRejectsMissingResponse(t *testing.T) {
	t.Parallel()

	if _, err := yagoproto.ParseQueryResponse(yagomodel.Message{}); err == nil {
		t.Fatal("expected error for missing response")
	}
}

func TestParseQueryResponseRejectsBadHeader(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldUptime: "later"}
	if _, err := yagoproto.ParseQueryResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
}
