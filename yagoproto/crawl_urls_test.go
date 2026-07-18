package yagoproto_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestCrawlURLRequestParsesFields(t *testing.T) {
	form := url.Values{
		yagoproto.FieldNetworkName: {"freeworld"},
		yagoproto.FieldIam:         {"sender-hash"},
		yagoproto.FieldYouAre:      {"self-hash"},
		yagoproto.FieldKey:         {"key"},
		yagoproto.FieldMagicMD5:    {"magic"},
		yagoproto.FieldMyTime:      {"20260101000000"},
		yagoproto.FieldCall:        {string(yagoproto.CrawlURLCallRemoteCrawl)},
		yagoproto.FieldCount:       {"7"},
		yagoproto.FieldTime:        {"9000"},
		yagoproto.FieldHashes:      {"ABCDEFGHIJKL"},
	}

	req, err := yagoproto.ParseCrawlURLRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	count, countOK := req.Count.Get()
	timeout, timeoutOK := req.Time.Get()
	if req.NetworkName != "freeworld" || req.Iam != "sender-hash" ||
		req.YouAre != "self-hash" || req.Key != "key" ||
		req.MagicMD5 != "magic" || req.MyTime != "20260101000000" ||
		req.Call != yagoproto.CrawlURLCallRemoteCrawl ||
		!countOK || count != 7 || !timeoutOK || timeout != 9000 ||
		req.Hashes != "ABCDEFGHIJKL" {
		t.Fatalf("request = %+v", req)
	}
}

func TestCrawlURLRequestFormRoundTrip(t *testing.T) {
	original := yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         "sender-hash",
		YouAre:      "self-hash",
		Key:         "key",
		MagicMD5:    "magic",
		MyTime:      "20260101000000",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Count:       yagomodel.Some(4),
		Time:        yagomodel.Some(2000),
		Hashes:      "ABCDEFGHIJKLMNOPQRSTUVWX",
	}

	parsed, err := yagoproto.ParseCrawlURLRequest(t.Context(), original.Form())
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
	req := yagoproto.CrawlURLRequest{Hashes: "ABCDEFGHIJKL!!!!!!!!!!!!"}

	hashes, ok := req.HashList()
	if !ok {
		t.Fatal("HashList rejected two 12-byte segments")
	}
	if len(hashes) != 2 || hashes[0] != "ABCDEFGHIJKL" || hashes[1] != "!!!!!!!!!!!!" {
		t.Fatalf("hashes = %v", hashes)
	}

	if _, ok := (yagoproto.CrawlURLRequest{Hashes: "short"}).HashList(); ok {
		t.Fatal("HashList accepted non-multiple length")
	}
}

func TestCrawlURLRequestHashListBoundsEntries(t *testing.T) {
	hash := "ABCDEFGHIJKL"
	hashes, ok := (yagoproto.CrawlURLRequest{
		Hashes: strings.Repeat(hash, yagoproto.MaximumCrawlURLHashes),
	}).HashList()
	if !ok || len(hashes) != yagoproto.MaximumCrawlURLHashes {
		t.Fatalf("boundary hashes = %d/%t", len(hashes), ok)
	}
	if hashes, ok := (yagoproto.CrawlURLRequest{
		Hashes: strings.Repeat(hash, yagoproto.MaximumCrawlURLHashes+1),
	}).HashList(); ok || hashes != nil {
		t.Fatalf("oversized hashes = %d/%t, want rejected", len(hashes), ok)
	}
}

func TestCrawlURLRequestRejectsBadNumericFields(t *testing.T) {
	if _, err := yagoproto.ParseCrawlURLRequest(
		t.Context(),
		url.Values{yagoproto.FieldCount: {"many"}},
	); err == nil {
		t.Fatal("expected bad count error")
	}

	if _, err := yagoproto.ParseCrawlURLRequest(
		t.Context(),
		url.Values{yagoproto.FieldTime: {"soon"}},
	); err == nil {
		t.Fatal("expected bad time error")
	}
}
