package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestSearchRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.SearchRequest{
		NetworkName: yacyproto.DefaultNetwork,
		MySeed:      sampleSeed(t, "alpha", "peer-a"),
		Query: []yacymodel.Hash{
			sampleHash(t, "alpha"),
			sampleHash(t, "beta"),
		},
		Exclude: []yacymodel.Hash{
			sampleHash(t, "gamma"),
		},
		URLs: []yacymodel.Hash{
			sampleHash(t, "url-a"),
		},
		Count:            10,
		Time:             3000,
		MaxDist:          5,
		Partitions:       30,
		Abstracts:        yacyproto.SearchAbstractsAuto,
		ContentDom:       yacyproto.ContentDomainText,
		StrictContentDom: true,
		TimezoneOffset:   120,
		Language:         "en",
		Author:           "ada",
		Protocol:         "https",
	}

	got, err := yacyproto.ParseSearchRequest(req.Form())
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}

	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, req)
	}
}

func TestSearchResponseRoundTrip(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	resp := yacyproto.SearchResponse{
		ResponseHeader: yacyproto.ResponseHeader{Version: "1.0", Uptime: 11},
		SearchTime:     120,
		References:     "topic",
		JoinCount:      4,
		Count:          2,
		Resources: []yacymodel.URIMetadataRow{
			sampleURLRow(t, "url-a"),
			sampleURLRow(t, "url-b"),
		},
		IndexCount:    map[yacymodel.Hash]int{alpha: 17},
		IndexAbstract: map[yacymodel.Hash]string{alpha: "abc"},
	}

	got, err := yacyproto.ParseSearchResponse(resp.Encode())
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}

	if !reflect.DeepEqual(got, resp) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, resp)
	}
}

func TestParseSearchRequestTruncatesRaggedQuery(t *testing.T) {
	t.Parallel()

	full := sampleHash(t, "alpha").String()
	form := url.Values{yacyproto.FieldQuery: {full + "tooshort"}}
	req, err := yacyproto.ParseSearchRequest(form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	if len(req.Query) != 1 {
		t.Fatalf("Query = %d, want 1 (trailing partial ignored)", len(req.Query))
	}
}

func TestParseSearchRequestTruncatesRaggedExclude(t *testing.T) {
	t.Parallel()

	full := sampleHash(t, "alpha").String()
	form := url.Values{yacyproto.FieldExclude: {full + "tooshort"}}
	req, err := yacyproto.ParseSearchRequest(form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	if len(req.Exclude) != 1 {
		t.Fatalf("Exclude = %d, want 1 (trailing partial ignored)", len(req.Exclude))
	}
}

func TestParseSearchRequestRejectsUnknownContentDomain(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldContentDom: {"binary"}}
	if _, err := yacyproto.ParseSearchRequest(form); err == nil {
		t.Fatal("expected error for unknown content domain")
	}
}

func TestParseSearchRequestRejectsUnknownStrictContentDom(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldStrictContentDom: {"yes"}}
	if _, err := yacyproto.ParseSearchRequest(form); err == nil {
		t.Fatal("expected error for unknown boolean token")
	}
}

func TestSearchResponseUsesYaCyLinkCountField(t *testing.T) {
	t.Parallel()

	msg := yacymodel.Message{yacyproto.FieldLinkCount: "5"}
	got, err := yacyproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}

	if got.Count != 5 {
		t.Fatalf("count = %d, want 5", got.Count)
	}
}
