package yacyproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestCrawlURLRequestParsesFields(t *testing.T) {
	form := url.Values{
		yacyproto.FieldNetworkName: {"freeworld"},
		yacyproto.FieldIam:         {"sender-hash"},
		yacyproto.FieldYouAre:      {"self-hash"},
		yacyproto.FieldKey:         {"key"},
		yacyproto.FieldMagicMD5:    {"magic"},
		yacyproto.FieldMyTime:      {"20260101000000"},
		yacyproto.FieldCall:        {string(yacyproto.CrawlURLCallRemoteCrawl)},
		yacyproto.FieldCount:       {"7"},
		yacyproto.FieldTime:        {"9000"},
		yacyproto.FieldHashes:      {"ABCDEFGHIJKL"},
	}

	req, err := yacyproto.ParseCrawlURLRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	count, countOK := req.Count.Get()
	timeout, timeoutOK := req.Time.Get()
	if req.NetworkName != "freeworld" || req.Iam != "sender-hash" ||
		req.YouAre != "self-hash" || req.Key != "key" ||
		req.MagicMD5 != "magic" || req.MyTime != "20260101000000" ||
		req.Call != yacyproto.CrawlURLCallRemoteCrawl ||
		!countOK || count != 7 || !timeoutOK || timeout != 9000 ||
		req.Hashes != "ABCDEFGHIJKL" {
		t.Fatalf("request = %+v", req)
	}
}

func TestCrawlURLRequestFormRoundTrip(t *testing.T) {
	original := yacyproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         "sender-hash",
		YouAre:      "self-hash",
		Key:         "key",
		MagicMD5:    "magic",
		MyTime:      "20260101000000",
		Call:        yacyproto.CrawlURLCallURLHashList,
		Count:       yacymodel.Some(4),
		Time:        yacymodel.Some(2000),
		Hashes:      "ABCDEFGHIJKLMNOPQRSTUVWX",
	}

	parsed, err := yacyproto.ParseCrawlURLRequest(t.Context(), original.Form())
	if err != nil {
		t.Fatal(err)
	}

	count, countOK := parsed.Count.Get()
	timeout, timeoutOK := parsed.Time.Get()
	if parsed.NetworkName != original.NetworkName || parsed.Iam != original.Iam ||
		parsed.YouAre != original.YouAre || parsed.Key != original.Key ||
		parsed.MagicMD5 != original.MagicMD5 || parsed.MyTime != original.MyTime ||
		parsed.Call != original.Call || parsed.Hashes != original.Hashes ||
		!countOK || count != 4 || !timeoutOK || timeout != 2000 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestCrawlURLRequestHashListUsesYaCyLengthRule(t *testing.T) {
	req := yacyproto.CrawlURLRequest{Hashes: "ABCDEFGHIJKL!!!!!!!!!!!!"}

	hashes, ok := req.HashList()
	if !ok {
		t.Fatal("HashList rejected two 12-byte segments")
	}
	if len(hashes) != 2 || hashes[0] != "ABCDEFGHIJKL" || hashes[1] != "!!!!!!!!!!!!" {
		t.Fatalf("hashes = %v", hashes)
	}

	if _, ok := (yacyproto.CrawlURLRequest{Hashes: "short"}).HashList(); ok {
		t.Fatal("HashList accepted non-multiple length")
	}
}

func TestCrawlURLRequestRejectsBadNumericFields(t *testing.T) {
	if _, err := yacyproto.ParseCrawlURLRequest(
		t.Context(),
		url.Values{yacyproto.FieldCount: {"many"}},
	); err == nil {
		t.Fatal("expected bad count error")
	}

	if _, err := yacyproto.ParseCrawlURLRequest(
		t.Context(),
		url.Values{yacyproto.FieldTime: {"soon"}},
	); err == nil {
		t.Fatal("expected bad time error")
	}
}
