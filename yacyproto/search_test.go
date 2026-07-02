package yacyproto_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestSearchRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yacyproto.SearchRequest{
		NetworkName: yacyproto.DefaultNetwork,
		MySeed:      yacymodel.Some(sampleSeed(t, "alpha", "peer-a")),
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

	got, err := yacyproto.ParseSearchRequest(t.Context(), req.Form())
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

	msg := resp.Encode()
	yacyproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yacyproto.ParseSearchResponse(msg)
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
	req, err := yacyproto.ParseSearchRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	if len(req.Query) != 1 {
		t.Fatalf("Query = %d, want 1 (trailing partial ignored)", len(req.Query))
	}
}

func TestSearchRequestFormOmitsEmptyHashLists(t *testing.T) {
	t.Parallel()

	form := (yacyproto.SearchRequest{}).Form()
	if form.Has(yacyproto.FieldQuery) || form.Has(yacyproto.FieldExclude) ||
		form.Has(yacyproto.FieldURLs) {
		t.Fatalf("empty hash fields should be omitted: %v", form)
	}
}

func TestParseSearchRequestTruncatesRaggedExclude(t *testing.T) {
	t.Parallel()

	full := sampleHash(t, "alpha").String()
	form := url.Values{yacyproto.FieldExclude: {full + "tooshort"}}
	req, err := yacyproto.ParseSearchRequest(t.Context(), form)
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
	if _, err := yacyproto.ParseSearchRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for unknown content domain")
	}
}

func TestParseSearchRequestRejectsUnknownStrictContentDom(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldStrictContentDom: {"yes"}}
	if _, err := yacyproto.ParseSearchRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for unknown boolean token")
	}
}

func TestParseSearchRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yacyproto.FieldMySeed: {"z|@@@"}},
		{yacyproto.FieldQuery: {"!!!!!!!!!!!!"}},
		{yacyproto.FieldExclude: {"!!!!!!!!!!!!"}},
		{yacyproto.FieldURLs: {"!!!!!!!!!!!!"}},
		{yacyproto.FieldAbstracts: {"!!!!!!!!!!!!"}},
		{yacyproto.FieldCount: {"many"}},
		{yacyproto.FieldTime: {"many"}},
		{yacyproto.FieldMaxDist: {"many"}},
		{yacyproto.FieldPartitions: {"many"}},
		{yacyproto.FieldTimezoneOffset: {"many"}},
	}
	for _, form := range cases {
		if _, err := yacyproto.ParseSearchRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseSearchRequest(%v) should fail", form)
		}
	}
}

func TestSearchAbstractHashes(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	beta := sampleHash(t, "beta")
	abstracts := yacyproto.SearchAbstracts(alpha.String() + beta.String())
	got := abstracts.Hashes()
	if len(got) != 2 || got[0] != alpha || got[1] != beta {
		t.Fatalf("Hashes = %v", got)
	}
	if got := yacyproto.SearchAbstracts("!!!!!!!!!!!!").Hashes(); got != nil {
		t.Fatalf("bad abstract hashes = %v", got)
	}
}

func TestParseSearchRequestAcceptsExplicitAbstractHashes(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	req, err := yacyproto.ParseSearchRequest(
		t.Context(),
		url.Values{yacyproto.FieldAbstracts: {alpha.String()}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if req.Abstracts != yacyproto.SearchAbstracts(alpha.String()) {
		t.Fatalf("Abstracts = %q", req.Abstracts)
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

func TestParseSearchResponseRejectsBadFields(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	cases := []yacymodel.Message{
		{yacyproto.FieldUptime: "soon"},
		{yacyproto.FieldSearchTime: "soon"},
		{yacyproto.FieldJoinCount: "many"},
		{yacyproto.FieldLinkCount: "many"},
		{"indexcount.short": "1"},
		{"indexcount." + alpha.String(): "many"},
		{"indexabstract.short": "abc"},
	}
	for _, msg := range cases {
		if _, err := yacyproto.ParseSearchResponse(msg); err == nil {
			t.Fatalf("ParseSearchResponse(%v) should fail", msg)
		}
	}
}

func TestParseSearchResponseSkipsMissingAndBadResources(t *testing.T) {
	t.Parallel()

	valid := sampleURLRow(t, "url-a")
	msg := yacymodel.Message{
		yacyproto.FieldLinkCount: "3",
		"resource0":              valid.String(),
		"resource2":              "bad",
	}
	got, err := yacyproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	if len(got.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(got.Resources))
	}
	if !reflect.DeepEqual(got.Resources[0], valid) {
		t.Fatalf("resource = %#v, want %#v", got.Resources[0], valid)
	}
}

func TestParseSearchResponseReadsOpenEndedResources(t *testing.T) {
	t.Parallel()

	valid := sampleURLRow(t, "url-a")
	msg := yacymodel.Message{
		"resource0": valid.String(),
		"resource1": "bad",
	}
	got, err := yacyproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	if len(got.Resources) != 1 || !reflect.DeepEqual(got.Resources[0], valid) {
		t.Fatalf("resources = %+v", got.Resources)
	}
}
