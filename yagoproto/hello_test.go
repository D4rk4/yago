package yagoproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestHelloRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.HelloRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Key:         "salt",
		Seed:        sampleSeed(t, "alpha", "peer-a"),
		Count:       50,
		Iam:         sampleHash(t, "alpha"),
		MagicMD5:    yagoproto.MagicMD5("k", "iam", "ess"),
		MyTime:      "20260617120000",
	}

	got, err := yagoproto.ParseHelloRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseHelloRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestHelloResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yagoproto.HelloResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.0", Uptime: 42},
		YourIP:         "203.0.113.7",
		YourType:       yagomodel.PeerSenior,
		MyTime:         "20260617120001",
		Message:        "ok",
		Seeds: []yagomodel.Seed{
			sampleSeed(t, "alpha", "peer-self"),
			sampleSeed(t, "beta", "peer-b"),
		},
	}

	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseHelloResponse(t.Context(), msg)
	if err != nil {
		t.Fatalf("ParseHelloResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}

	if own, ok := got.OwnSeed().Get(); !ok || own.Hash != resp.Seeds[0].Hash {
		t.Fatalf("OwnSeed = %v, %v", own, ok)
	}
	if known := got.KnownSeeds(); len(known) != 1 || known[0].Hash != resp.Seeds[1].Hash {
		t.Fatalf("KnownSeeds = %+v", known)
	}
}

func TestHelloResponseSeedAccessorsOnEmptyResponse(t *testing.T) {
	resp := yagoproto.HelloResponse{}
	if _, ok := resp.OwnSeed().Get(); ok {
		t.Fatal("empty response OwnSeed should be absent")
	}
	if known := resp.KnownSeeds(); known != nil {
		t.Fatalf("empty response KnownSeeds = %+v", known)
	}
}

func TestParseHelloRequestRejectsBadIam(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldSeed: {
			yagomodel.EncodeCompactWireForm(sampleSeed(t, "alpha", "peer-a").String()),
		},
		yagoproto.FieldIam: {"short"},
	}
	if _, err := yagoproto.ParseHelloRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseHelloRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yagoproto.FieldCount: {"many"}},
		{},
		{yagoproto.FieldSeed: {"z|@@@"}},
		{yagoproto.FieldSeed: {yagomodel.EncodeCompactWireForm("{Hash=short}")}},
	}
	for _, form := range cases {
		if _, err := yagoproto.ParseHelloRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseHelloRequest(%v) should fail", form)
		}
	}
}

func TestParseHelloResponseRejectsBadPeerType(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldYourType: "overlord"}
	if _, err := yagoproto.ParseHelloResponse(t.Context(), msg); err == nil {
		t.Fatal("expected error for unknown peer type")
	}
}

func TestParseHelloResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yagomodel.Message{
		{yagoproto.FieldUptime: "soon"},
		{"seed0": "z|@@@"},
		{"seed0": yagomodel.EncodeCompactWireForm("{Hash=short}")},
	}
	for _, msg := range cases {
		if _, err := yagoproto.ParseHelloResponse(t.Context(), msg); err == nil {
			t.Fatalf("ParseHelloResponse(%v) should fail", msg)
		}
	}
}
