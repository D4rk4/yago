package yagoproto_test

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestHelloRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.HelloRequest{
		NetworkName:        yagoproto.DefaultNetwork,
		NetworkNamePresent: true,
		Key:                "salt",
		Seed:               sampleSeed(t, "alpha", "peer-a"),
		Count:              50,
		Iam:                sampleHash(t, "alpha").String(),
		MagicMD5:           yagoproto.MagicMD5("k", "iam", "ess"),
		MyTime:             "20260617120000",
	}

	got, err := yagoproto.ParseHelloRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseHelloRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestHelloRequestPreservesNetworkNamePresence(t *testing.T) {
	t.Parallel()

	seed := sampleSeed(t, "alpha", "peer-a")
	for _, test := range []struct {
		name    string
		form    url.Values
		present bool
	}{
		{
			name: "absent",
			form: url.Values{
				yagoproto.FieldSeed: {yagomodel.EncodeCompactWireForm(seed.String())},
			},
		},
		{
			name: "present empty",
			form: url.Values{
				yagoproto.FieldNetworkName: {""},
				yagoproto.FieldSeed:        {yagomodel.EncodeCompactWireForm(seed.String())},
			},
			present: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := yagoproto.ParseHelloRequest(t.Context(), test.form)
			if err != nil {
				t.Fatalf("ParseHelloRequest: %v", err)
			}
			if request.NetworkNamePresent != test.present {
				t.Fatalf(
					"NetworkNamePresent = %v, want %v",
					request.NetworkNamePresent,
					test.present,
				)
			}
			if request.Form().Has(yagoproto.FieldNetworkName) != test.present {
				t.Fatalf(
					"round-trip network presence = %v, want %v",
					request.Form().Has(yagoproto.FieldNetworkName),
					test.present,
				)
			}
		})
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

func TestParseHelloRequestPreservesOpaqueIam(t *testing.T) {
	t.Parallel()

	form := url.Values{
		yagoproto.FieldSeed: {
			yagomodel.EncodeCompactWireForm(sampleSeed(t, "alpha", "peer-a").String()),
		},
		yagoproto.FieldIam: {"short"},
	}
	request, err := yagoproto.ParseHelloRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseHelloRequest: %v", err)
	}
	if request.Iam != "short" {
		t.Fatalf("Iam = %q, want short", request.Iam)
	}
}

func TestParseHelloRequestFallsMalformedCountBackToZero(t *testing.T) {
	t.Parallel()

	for _, count := range []string{"many", "2147483648", "-2147483649"} {
		form := url.Values{
			yagoproto.FieldSeed: {
				yagomodel.EncodeCompactWireForm(sampleSeed(t, "alpha", "peer-a").String()),
			},
			yagoproto.FieldCount: {count},
		}
		request, err := yagoproto.ParseHelloRequest(t.Context(), form)
		if err != nil {
			t.Fatalf("ParseHelloRequest count %q: %v", count, err)
		}
		if request.Count != 0 {
			t.Fatalf("Count for %q = %d, want 0", count, request.Count)
		}
	}
}

func TestParseHelloRequestUsesJavaSignedDecimalInt32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  int
	}{
		{value: "١٢٣", want: 123},
		{value: "４２", want: 42},
		{value: "+2147483647", want: 2147483647},
		{value: "-2147483648", want: -2147483648},
		{value: "2147483648", want: 0},
		{value: "𝟙", want: 0},
	}
	for _, test := range tests {
		form := url.Values{
			yagoproto.FieldSeed: {
				yagomodel.EncodeCompactWireForm(sampleSeed(t, "alpha", "peer-a").String()),
			},
			yagoproto.FieldCount: {test.value},
		}
		request, err := yagoproto.ParseHelloRequest(t.Context(), form)
		if err != nil {
			t.Fatalf("ParseHelloRequest count %q: %v", test.value, err)
		}
		if request.Count != test.want {
			t.Fatalf("Count for %q = %d, want %d", test.value, request.Count, test.want)
		}
	}
}

func TestParseHelloRequestEnforcesStockSeedBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		units         int
		supplementary bool
		valid         bool
	}{
		{name: "exact ASCII", units: yagoproto.HelloSeedMaximumUTF16Units, valid: true},
		{name: "above ASCII", units: yagoproto.HelloSeedMaximumUTF16Units + 1},
		{
			name:          "exact supplementary",
			units:         yagoproto.HelloSeedMaximumUTF16Units,
			supplementary: true,
			valid:         true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := helloSeedWithUTF16Units(t, test.units, test.supplementary)
			form := url.Values{yagoproto.FieldSeed: {raw}}
			_, err := yagoproto.ParseHelloRequest(t.Context(), form)
			if (err == nil) != test.valid {
				t.Fatalf("ParseHelloRequest seed units %d error = %v", test.units, err)
			}
			if test.supplementary && len(raw) <= test.units {
				t.Fatalf("supplementary seed bytes = %d, want above %d", len(raw), test.units)
			}
		})
	}
}

func helloSeedWithUTF16Units(t *testing.T, units int, supplementary bool) string {
	t.Helper()

	prefix := "p|{Hash=" + sampleHash(t, "hello-boundary").String() + ",a="
	suffix := ",b=}"
	extra := ""
	if supplementary {
		extra = "😀"
	}
	remaining := units - len(utf16.Encode([]rune(prefix+extra+suffix)))
	first := min(8000, remaining)
	raw := prefix + strings.Repeat("a", first) + extra +
		",b=" + strings.Repeat("b", remaining-first) + "}"
	if got := len(utf16.Encode([]rune(raw))); got != units {
		t.Fatalf("seed UTF-16 units = %d, want %d", got, units)
	}

	return raw
}

func TestParseHelloRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
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

func TestParseHelloResponseSkipsMalformedKnownSeeds(t *testing.T) {
	t.Parallel()

	self := sampleSeed(t, "alpha", "peer-self")
	known := sampleSeed(t, "beta", "peer-known")
	msg := yagomodel.Message{
		"seed0": yagomodel.EncodeCompactWireForm(self.String()),
		"seed1": "z|@@@",
		"seed2": yagomodel.EncodeCompactWireForm(known.String()),
	}

	response, err := yagoproto.ParseHelloResponse(t.Context(), msg)
	if err != nil {
		t.Fatalf("ParseHelloResponse: %v", err)
	}
	if own, ok := response.OwnSeed().Get(); !ok || own.Hash != self.Hash {
		t.Fatalf("OwnSeed = %#v/%v", own, ok)
	}
	if seeds := response.KnownSeeds(); len(seeds) != 1 || seeds[0].Hash != known.Hash {
		t.Fatalf("KnownSeeds = %#v", seeds)
	}
}

func TestParseHelloResponseStopsOnCancellationAtMalformedKnownSeed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	self := sampleSeed(t, "alpha", "peer-self")
	msg := yagomodel.Message{
		"seed0": yagomodel.EncodeCompactWireForm(self.String()),
		"seed1": "z|@@@",
	}

	if _, err := yagoproto.ParseHelloResponse(ctx, msg); !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseHelloResponse error = %v, want context canceled", err)
	}
}
