package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestHelloRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.HelloRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Key:         "salt",
		Seed:        sampleSeed(t, "alpha", "peer-a"),
		Count:       50,
		Iam:         sampleHash(t, "alpha"),
		MagicMD5:    yacyproto.MagicMD5("k", "iam", "ess"),
		MyTime:      "20260617120000",
	}

	got, err := yacyproto.ParseHelloRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseHelloRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestHelloResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := yacyproto.HelloResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 42},
		YourIP:         "203.0.113.7",
		YourType:       yacymodel.PeerSenior,
		MyTime:         "20260617120001",
		Message:        "ok",
		Seeds: []yacymodel.Seed{
			sampleSeed(t, "alpha", "peer-self"),
			sampleSeed(t, "beta", "peer-b"),
		},
	}

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseHelloResponse(t.Context(), msg)
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
	resp := yacyproto.HelloResponse{}
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
		yacyproto.FieldSeed: {
			yacymodel.EncodeCompactWireForm(sampleSeed(t, "alpha", "peer-a").String()),
		},
		yacyproto.FieldIam: {"short"},
	}
	if _, err := yacyproto.ParseHelloRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for malformed iam hash")
	}
}

func TestParseHelloRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yacyproto.FieldCount: {"many"}},
		{},
		{yacyproto.FieldSeed: {"z|@@@"}},
		{yacyproto.FieldSeed: {yacymodel.EncodeCompactWireForm("{Hash=short}")}},
	}
	for _, form := range cases {
		if _, err := yacyproto.ParseHelloRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseHelloRequest(%v) should fail", form)
		}
	}
}

func TestParseHelloResponseRejectsBadPeerType(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldYourType: "overlord"}
	if _, err := yacyproto.ParseHelloResponse(t.Context(), msg); err == nil {
		t.Fatal("expected error for unknown peer type")
	}
}

func TestParseHelloResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []yacymodel.Message{
		{yacyproto.FieldUptime: "soon"},
		{"seed0": "z|@@@"},
		{"seed0": yacymodel.EncodeCompactWireForm("{Hash=short}")},
	}
	for _, msg := range cases {
		if _, err := yacyproto.ParseHelloResponse(t.Context(), msg); err == nil {
			t.Fatalf("ParseHelloResponse(%v) should fail", msg)
		}
	}
}
