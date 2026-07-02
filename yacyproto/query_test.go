package yacyproto_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestQueryRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.QueryRequest{
		NetworkName: yacyproto.DefaultNetwork,
		YouAre:      sampleHash(t, "alpha"),
		Iam:         sampleHash(t, "beta"),
		Object:      yacyproto.ObjectRWIURLCount,
		Env:         sampleHash(t, "alpha").String(),
	}

	got, err := yacyproto.ParseQueryRequest(context.Background(), req.Form())
	if err != nil {
		t.Fatalf("ParseQueryRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestQueryResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yacyproto.QueryResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 3},
		Response:       yacyproto.QueryResponseRejected,
		MyTime:         "20260617120002",
		Magic:          "deadbeef",
	}

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseQueryResponse(msg)
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
		yacyproto.FieldObject: {string(yacyproto.ObjectRWICount)},
		yacyproto.FieldIam:    {"nope"},
	}
	if _, err := yacyproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseQueryRequestRejectsBadYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yacyproto.FieldObject: {string(yacyproto.ObjectRWICount)},
		yacyproto.FieldYouAre: {"nope"},
	}
	if _, err := yacyproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for malformed youare hash")
	}
}

func TestParseQueryRequestRejectsUnknownObject(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldObject: {"whatever"}}
	if _, err := yacyproto.ParseQueryRequest(context.Background(), form); err == nil {
		t.Fatal("expected error for unknown query object")
	}
}

func TestParseQueryResponseRejectsBadResponse(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldResponse: "many"}
	if _, err := yacyproto.ParseQueryResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric response")
	}
}

func TestParseQueryResponseRejectsBadHeader(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldUptime: "later"}
	if _, err := yacyproto.ParseQueryResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
}
