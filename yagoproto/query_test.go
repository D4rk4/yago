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
		NetworkName:        yagoproto.DefaultNetwork,
		NetworkNamePresent: true,
		YouAre:             sampleHash(t, "alpha").String(),
		Iam:                sampleHash(t, "beta").String(),
		Object:             yagoproto.ObjectRWIURLCount,
		Env:                sampleHash(t, "alpha").String(),
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
		NetworkName:        yagoproto.DefaultNetwork,
		NetworkNamePresent: true,
		YouAre:             sampleHash(t, "alpha").String(),
		Object:             yagoproto.ObjectRWICount,
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

func TestParseQueryRequestPreservesArbitraryIam(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldObject: {string(yagoproto.ObjectRWICount)},
		yagoproto.FieldYouAre: {sampleHash(t, "alpha").String()},
		yagoproto.FieldIam:    {"nope"},
	}
	request, err := yagoproto.ParseQueryRequest(context.Background(), form)
	if err != nil || request.Iam != "nope" {
		t.Fatalf("ParseQueryRequest = %#v, %v", request, err)
	}
}

func TestParseQueryRequestPreservesArbitraryYouAre(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldObject: {string(yagoproto.ObjectRWICount)},
		yagoproto.FieldYouAre: {"nope"},
	}
	request, err := yagoproto.ParseQueryRequest(context.Background(), form)
	if err != nil || request.YouAre != "nope" {
		t.Fatalf("ParseQueryRequest = %#v, %v", request, err)
	}
}

func TestParseQueryRequestPreservesUnknownObject(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldObject: {"whatever"},
		yagoproto.FieldYouAre: {sampleHash(t, "alpha").String()},
	}
	request, err := yagoproto.ParseQueryRequest(context.Background(), form)
	if err != nil || request.Object != yagoproto.QueryObject("whatever") {
		t.Fatalf("ParseQueryRequest = %#v, %v", request, err)
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

	_, err := yagoproto.ParseQueryResponse(yagomodel.Message{
		yagoproto.FieldMagic:  "123",
		yagoproto.FieldMyTime: "20260721120000",
	})
	if err == nil {
		t.Fatal("missing response field was accepted")
	}
}

func TestQueryResponseCanEmitUnresolvedTemplateMarker(t *testing.T) {
	t.Parallel()

	response := yagoproto.QueryResponse{
		Magic: "123", MyTime: "20260721120000", UnresolvedResponse: true,
	}
	message := response.Encode()
	if message[yagoproto.FieldResponse] != yagoproto.QueryResponseUnresolved {
		t.Fatalf("encoded response = %#v", message)
	}
	parsed, err := yagoproto.ParseQueryResponse(message)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.UnresolvedResponse || parsed.Response != yagoproto.QueryResponseRejected ||
		parsed.Magic != response.Magic || parsed.MyTime != response.MyTime {
		t.Fatalf("parsed response = %#v", parsed)
	}
}

func TestQueryResponseEmitsUnresolvedTimeWhenAuthenticationStopsEarly(t *testing.T) {
	t.Parallel()

	message := (yagoproto.QueryResponse{
		Response: yagoproto.QueryResponseRejected,
		Magic:    "123",
	}).Encode()
	if message[yagoproto.FieldMyTime] != yagoproto.QueryResponseUnresolved {
		t.Fatalf("mytime = %q", message[yagoproto.FieldMyTime])
	}
}

func TestParseQueryResponseRejectsBadHeader(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldUptime: "later"}
	if _, err := yagoproto.ParseQueryResponse(msg); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
}
