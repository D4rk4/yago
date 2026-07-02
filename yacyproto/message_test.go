package yacyproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestMessageRequestPermissionFormRoundTrip(t *testing.T) {
	req := yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor(t, "youare"),
		Iam:         hashFor(t, "sender"),
		Key:         "salt",
		MagicMD5:    "magic",
		MyTime:      "20260701210000",
		Process:     yacyproto.MessageProcessPermission,
	}

	got, err := yacyproto.ParseMessageRequest(t.Context(), req.Form())
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
	seed := yacymodel.Seed{
		Hash: hashFor(t, "sender"),
		Name: yacymodel.Some("sender-peer"),
	}
	form := url.Values{
		yacyproto.FieldMessageProcess: {string(yacyproto.MessageProcessPost)},
		yacyproto.FieldMySeed:         {yacymodel.EncodeCompactWireForm(seed.String())},
		yacyproto.FieldMessageSubject: {yacymodel.EncodeBase64WireForm("hello")},
		yacyproto.FieldMessage:        {yacymodel.EncodeBase64WireForm("body")},
	}

	got, err := yacyproto.ParseMessageRequest(t.Context(), form)
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

	roundTrip, err := yacyproto.ParseMessageRequest(t.Context(), got.Form())
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Subject != got.Subject || roundTrip.Body != got.Body {
		t.Fatalf("Form round-trip subject/body = %q/%q", roundTrip.Subject, roundTrip.Body)
	}
}

func TestMessageRequestPermissionIgnoresPostFields(t *testing.T) {
	form := url.Values{
		yacyproto.FieldYouAre:         {hashFor(t, "youare").String()},
		yacyproto.FieldIam:            {hashFor(t, "sender").String()},
		yacyproto.FieldMessageProcess: {string(yacyproto.MessageProcessPermission)},
		yacyproto.FieldMySeed:         {yacymodel.EncodeCompactWireForm("{Hash=short}")},
		yacyproto.FieldMessageSubject: {"z|@@@"},
		yacyproto.FieldMessage:        {"z|@@@"},
	}

	got, err := yacyproto.ParseMessageRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	if got.Process != yacyproto.MessageProcessPermission {
		t.Fatalf("Process = %q, want permission", got.Process)
	}
	if _, ok := got.MySeed.Get(); ok {
		t.Fatal("MySeed should be ignored for permission")
	}
	if got.Subject != "" || got.Body != "" {
		t.Fatalf("Subject/Body = %q/%q, want empty", got.Subject, got.Body)
	}
}

func TestMessageRequestDefaultsToPermission(t *testing.T) {
	got, err := yacyproto.ParseMessageRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Process != yacyproto.MessageProcessPermission {
		t.Fatalf("Process = %q, want permission", got.Process)
	}
}

func TestMessageResponseEncodeRoundTrip(t *testing.T) {
	resp := yacyproto.MessageResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.940", Uptime: 42},
		MessageSize:    10240,
		AttachmentSize: 0,
		Response:       yacyproto.MessageResponsePermission,
	}
	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)

	got, err := yacyproto.ParseMessageResponse(msg)
	if err != nil {
		t.Fatal(err)
	}

	if got != resp {
		t.Fatalf("response = %+v, want %+v", got, resp)
	}
}

func TestParseMessageRequestRejectsBadFields(t *testing.T) {
	cases := []url.Values{
		{yacyproto.FieldYouAre: {"short"}},
		{
			yacyproto.FieldYouAre: {hashFor(t, "youare").String()},
			yacyproto.FieldIam:    {"short"},
		},
		{
			yacyproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yacyproto.FieldIam:            {hashFor(t, "sender").String()},
			yacyproto.FieldMessageProcess: {string(yacyproto.MessageProcessPost)},
			yacyproto.FieldMySeed:         {yacymodel.EncodeCompactWireForm("{Hash=short}")},
		},
		{
			yacyproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yacyproto.FieldIam:            {hashFor(t, "sender").String()},
			yacyproto.FieldMessageProcess: {string(yacyproto.MessageProcessPost)},
			yacyproto.FieldMessageSubject: {"z|@@@"},
		},
		{
			yacyproto.FieldYouAre:         {hashFor(t, "youare").String()},
			yacyproto.FieldIam:            {hashFor(t, "sender").String()},
			yacyproto.FieldMessageProcess: {string(yacyproto.MessageProcessPost)},
			yacyproto.FieldMessage:        {"z|@@@"},
		},
	}
	for _, form := range cases {
		if _, err := yacyproto.ParseMessageRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseMessageRequest(%v) should fail", form)
		}
	}
}

func TestParseMessageResponseRejectsBadFields(t *testing.T) {
	cases := []yacymodel.Message{
		{yacyproto.FieldUptime: "soon"},
		{yacyproto.FieldMessageSize: "large"},
		{yacyproto.FieldMessageAttachmentSize: "large"},
	}
	for _, msg := range cases {
		if _, err := yacyproto.ParseMessageResponse(msg); err == nil {
			t.Fatalf("ParseMessageResponse(%v) should fail", msg)
		}
	}
}

const hashFiller = "AAAAAAAAAAAA"

func hashFor(t testing.TB, base string) yacymodel.Hash {
	t.Helper()

	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}
