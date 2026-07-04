package yagoproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestMessageRequestPermissionFormRoundTrip(t *testing.T) {
	req := yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor(t, "youare"),
		Iam:         hashFor(t, "sender"),
		Key:         "salt",
		MagicMD5:    "magic",
		MyTime:      "20260701210000",
		Process:     yagoproto.MessageProcessPermission,
	}

	got, err := yagoproto.ParseMessageRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatal(err)
	}

	if got.NetworkName != req.NetworkName || got.YouAre != req.YouAre ||
		got.Iam != req.Iam || got.Key != req.Key || got.MagicMD5 != req.MagicMD5 ||
		got.MyTime != req.MyTime || got.Process != req.Process {
		t.Fatalf("request = %+v, want %+v", got, req)
	}
}

func TestMessageRequestPostDecodesSeedAndMessageFields(t *testing.T) {
	seed := yagomodel.Seed{
		Hash: hashFor(t, "sender"),
		Name: yagomodel.Some("sender-peer"),
	}
	form := url.Values{
		yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPost)},
		yagoproto.FieldMySeed:         {yagomodel.EncodeCompactWireForm(seed.String())},
		yagoproto.FieldMessageSubject: {yagomodel.EncodeBase64WireForm("hello")},
		yagoproto.FieldMessage:        {yagomodel.EncodeBase64WireForm("body")},
	}

	got, err := yagoproto.ParseMessageRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	gotSeed, ok := got.MySeed.Get()
	if !ok {
		t.Fatal("MySeed missing")
	}
	if gotSeed.Hash != seed.Hash {
		t.Fatalf("MySeed.Hash = %q, want %q", gotSeed.Hash, seed.Hash)
	}
	if got.Subject != "hello" || got.Body != "body" {
		t.Fatalf("decoded subject/body = %q/%q", got.Subject, got.Body)
	}

	roundTrip, err := yagoproto.ParseMessageRequest(t.Context(), got.Form())
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Subject != got.Subject || roundTrip.Body != got.Body {
		t.Fatalf("Form round-trip subject/body = %q/%q", roundTrip.Subject, roundTrip.Body)
	}
}

func TestMessageRequestPermissionIgnoresPostFields(t *testing.T) {
	form := url.Values{
		yagoproto.FieldYouAre:         {hashFor(t, "youare").String()},
		yagoproto.FieldIam:            {hashFor(t, "sender").String()},
		yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPermission)},
		yagoproto.FieldMySeed:         {yagomodel.EncodeCompactWireForm("{Hash=short}")},
		yagoproto.FieldMessageSubject: {"z|@@@"},
		yagoproto.FieldMessage:        {"z|@@@"},
	}

	got, err := yagoproto.ParseMessageRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	if got.Process != yagoproto.MessageProcessPermission {
		t.Fatalf("Process = %q, want permission", got.Process)
	}
	if _, ok := got.MySeed.Get(); ok {
		t.Fatal("MySeed should be ignored for permission")
	}
	if got.Subject != "" || got.Body != "" {
		t.Fatalf("Subject/Body = %q/%q, want empty", got.Subject, got.Body)
	}
}

func TestMessageRequestIgnoresMalformedIam(t *testing.T) {
	form := url.Values{
		yagoproto.FieldYouAre:         {hashFor(t, "youare").String()},
		yagoproto.FieldIam:            {"short"},
		yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPermission)},
	}

	got, err := yagoproto.ParseMessageRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	if got.Iam != "" {
		t.Fatalf("Iam = %q, want empty", got.Iam)
	}
	if got.Process != yagoproto.MessageProcessPermission {
		t.Fatalf("Process = %q, want permission", got.Process)
	}
}

func TestMessageRequestDefaultsToPermission(t *testing.T) {
	got, err := yagoproto.ParseMessageRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Process != yagoproto.MessageProcessPermission {
		t.Fatalf("Process = %q, want permission", got.Process)
	}
}

func TestMessageResponseEncodeRoundTrip(t *testing.T) {
	resp := yagoproto.MessageResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.940", Uptime: 42},
		MessageSize:    10240,
		AttachmentSize: 0,
		Response:       yagoproto.MessageResponsePermission,
	}
	msg := resp.Encode()
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)

	got, err := yagoproto.ParseMessageResponse(msg)
	if err != nil {
		t.Fatal(err)
	}

	if got != resp {
		t.Fatalf("response = %+v, want %+v", got, resp)
	}
}

func TestParseMessageRequestRejectsBadFields(t *testing.T) {
	cases := []url.Values{
		{yagoproto.FieldYouAre: {"short"}},
		{
			yagoproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yagoproto.FieldIam:            {hashFor(t, "sender").String()},
			yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPost)},
			yagoproto.FieldMySeed:         {yagomodel.EncodeCompactWireForm("{Hash=short}")},
		},
		{
			yagoproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yagoproto.FieldIam:            {hashFor(t, "sender").String()},
			yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPost)},
			yagoproto.FieldMessageSubject: {"z|@@@"},
		},
		{
			yagoproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yagoproto.FieldIam:            {hashFor(t, "sender").String()},
			yagoproto.FieldMessageProcess: {string(yagoproto.MessageProcessPost)},
			yagoproto.FieldMessage:        {"z|@@@"},
		},
	}
	for _, form := range cases {
		if _, err := yagoproto.ParseMessageRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseMessageRequest(%v) should fail", form)
		}
	}
}

func TestParseMessageResponseRejectsBadFields(t *testing.T) {
	cases := []yagomodel.Message{
		{yagoproto.FieldUptime: "soon"},
		{yagoproto.FieldMessageSize: "large"},
		{yagoproto.FieldMessageAttachmentSize: "large"},
	}
	for _, msg := range cases {
		if _, err := yagoproto.ParseMessageResponse(msg); err == nil {
			t.Fatalf("ParseMessageResponse(%v) should fail", msg)
		}
	}
}

const hashFiller = "AAAAAAAAAAAA"

func hashFor(t testing.TB, base string) yagomodel.Hash {
	t.Helper()

	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + hashFiller[len(base):])
}
