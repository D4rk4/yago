package yagoproto_test

import (
	"errors"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSearchRequestRoundTrip(t *testing.T) {
	t.Parallel()

	req := yagoproto.SearchRequest{
		NetworkName: yagoproto.DefaultNetwork,
		MySeed:      yagomodel.Some(sampleSeed(t, "alpha", "peer-a")),
		Query: []yagomodel.Hash{
			sampleHash(t, "alpha"),
			sampleHash(t, "beta"),
		},
		Exclude: []yagomodel.Hash{
			sampleHash(t, "gamma"),
		},
		URLs: []yagomodel.Hash{
			sampleHash(t, "url-a"),
		},
		Count:            10,
		Time:             3000,
		MaxDist:          5,
		Partitions:       30,
		Abstracts:        yagoproto.SearchAbstractsAuto,
		ContentDom:       yagoproto.ContentDomainText,
		StrictContentDom: true,
		TimezoneOffset:   120,
		Language:         "en",
		Author:           "ada",
		Protocol:         "https",
	}

	got, err := yagoproto.ParseSearchRequest(t.Context(), req.Form())
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
	resp := yagoproto.SearchResponse{
		ResponseHeader: yagoproto.ResponseHeader{Version: "1.0", Uptime: 11},
		SearchTime:     120,
		References:     "topic",
		JoinCount:      4,
		Count:          2,
		Resources: []yagomodel.URIMetadataRow{
			sampleURLRow(t, "url-a"),
			sampleURLRow(t, "url-b"),
		},
		IndexCount:    map[yagomodel.Hash]int{alpha: 17},
		IndexAbstract: map[yagomodel.Hash]string{alpha: "abc"},
	}

	msg := resp.Encode()
	if _, ok := msg[yagoproto.FieldCount]; !ok {
		t.Fatalf("encoded response missing %q: %v", yagoproto.FieldCount, msg)
	}
	if _, ok := msg[yagoproto.FieldLinkCount]; ok {
		t.Fatalf("encoded response should not expose %q: %v", yagoproto.FieldLinkCount, msg)
	}
	yagoproto.InjectResponseHeader(msg, resp.Version, resp.Uptime)
	got, err := yagoproto.ParseSearchResponse(msg)
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
	form := url.Values{yagoproto.FieldQuery: {full + "tooshort"}}
	req, err := yagoproto.ParseSearchRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	if len(req.Query) != 1 {
		t.Fatalf("Query = %d, want 1 (trailing partial ignored)", len(req.Query))
	}
}

func TestSearchRequestFormOmitsEmptyHashLists(t *testing.T) {
	t.Parallel()

	form := (yagoproto.SearchRequest{}).Form()
	if form.Has(yagoproto.FieldQuery) || form.Has(yagoproto.FieldExclude) ||
		form.Has(yagoproto.FieldURLs) {
		t.Fatalf("empty hash fields should be omitted: %v", form)
	}
}

func TestParseSearchRequestTruncatesRaggedExclude(t *testing.T) {
	t.Parallel()

	full := sampleHash(t, "alpha").String()
	form := url.Values{yagoproto.FieldExclude: {full + "tooshort"}}
	req, err := yagoproto.ParseSearchRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	if len(req.Exclude) != 1 {
		t.Fatalf("Exclude = %d, want 1 (trailing partial ignored)", len(req.Exclude))
	}
}

func TestParseSearchRequestRejectsUnknownContentDomain(t *testing.T) {
	t.Parallel()

	form := url.Values{yagoproto.FieldContentDom: {"binary"}}
	if _, err := yagoproto.ParseSearchRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for unknown content domain")
	}
}

func TestParseSearchRequestRejectsUnknownStrictContentDom(t *testing.T) {
	t.Parallel()

	form := url.Values{yagoproto.FieldStrictContentDom: {"yes"}}
	if _, err := yagoproto.ParseSearchRequest(t.Context(), form); err == nil {
		t.Fatal("expected error for unknown boolean token")
	}
}

func TestParseSearchRequestRejectsBadFields(t *testing.T) {
	t.Parallel()

	cases := []url.Values{
		{yagoproto.FieldMySeed: {"z|@@@"}},
		{yagoproto.FieldQuery: {"!!!!!!!!!!!!"}},
		{yagoproto.FieldExclude: {"!!!!!!!!!!!!"}},
		{yagoproto.FieldURLs: {"!!!!!!!!!!!!"}},
		{yagoproto.FieldAbstracts: {"!!!!!!!!!!!!"}},
		{yagoproto.FieldCount: {"many"}},
		{yagoproto.FieldTime: {"many"}},
		{yagoproto.FieldMaxDist: {"many"}},
		{yagoproto.FieldPartitions: {"many"}},
		{yagoproto.FieldTimezoneOffset: {"many"}},
	}
	for _, form := range cases {
		if _, err := yagoproto.ParseSearchRequest(t.Context(), form); err == nil {
			t.Fatalf("ParseSearchRequest(%v) should fail", form)
		}
	}
}

func TestParseSearchRequestBoundsHashFields(t *testing.T) {
	hash := sampleHash(t, "alpha").String()
	for _, item := range []struct {
		field string
		limit int
	}{
		{field: yagoproto.FieldQuery, limit: yagoproto.MaximumSearchTermHashes},
		{field: yagoproto.FieldExclude, limit: yagoproto.MaximumSearchTermHashes},
		{field: yagoproto.FieldAbstracts, limit: yagoproto.MaximumSearchTermHashes},
		{field: yagoproto.FieldURLs, limit: yagoproto.MaximumSearchURLHashes},
	} {
		t.Run(item.field, func(t *testing.T) {
			if _, err := yagoproto.ParseSearchRequest(
				t.Context(),
				url.Values{item.field: {strings.Repeat(hash, item.limit)}},
			); err != nil {
				t.Fatalf("parse boundary: %v", err)
			}
			if _, err := yagoproto.ParseSearchRequest(
				t.Context(),
				url.Values{item.field: {strings.Repeat(hash, item.limit+1)}},
			); !errors.Is(err, yagoproto.ErrBadField) {
				t.Fatalf("error = %v, want bad field", err)
			}
		})
	}
}

func TestParseSearchRequestRejectsExtremeHashFieldBeforeExpansion(t *testing.T) {
	raw := strings.Repeat(sampleHash(t, "alpha").String(), 100_000)
	if _, err := yagoproto.ParseSearchRequest(
		t.Context(),
		url.Values{yagoproto.FieldQuery: {raw}},
	); !errors.Is(err, yagoproto.ErrBadField) {
		t.Fatalf("error = %v, want bad field", err)
	}
}

func TestSearchAbstractHashes(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	beta := sampleHash(t, "beta")
	abstracts := yagoproto.SearchAbstracts(alpha.String() + beta.String())
	got := abstracts.Hashes()
	if len(got) != 2 || got[0] != alpha || got[1] != beta {
		t.Fatalf("Hashes = %v", got)
	}
	if got := yagoproto.SearchAbstracts("!!!!!!!!!!!!").Hashes(); got != nil {
		t.Fatalf("bad abstract hashes = %v", got)
	}
}

func TestParseSearchRequestAcceptsExplicitAbstractHashes(t *testing.T) {
	t.Parallel()

	alpha := sampleHash(t, "alpha")
	req, err := yagoproto.ParseSearchRequest(
		t.Context(),
		url.Values{yagoproto.FieldAbstracts: {alpha.String()}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if req.Abstracts != yagoproto.SearchAbstracts(alpha.String()) {
		t.Fatalf("Abstracts = %q", req.Abstracts)
	}
}

func TestSearchResponseUsesYaCyCountField(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldCount: "5"}
	got, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}

	if got.Count != 5 {
		t.Fatalf("count = %d, want 5", got.Count)
	}
}

func TestSearchResponseParsesLegacyLinkCountField(t *testing.T) {
	t.Parallel()

	msg := yagomodel.Message{yagoproto.FieldLinkCount: "5"}
	got, err := yagoproto.ParseSearchResponse(msg)
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
	cases := []yagomodel.Message{
		{yagoproto.FieldUptime: "soon"},
		{yagoproto.FieldSearchTime: "soon"},
		{yagoproto.FieldJoinCount: "many"},
		{yagoproto.FieldCount: "many"},
		{yagoproto.FieldLinkCount: "many"},
		{"indexcount.short": "1"},
		{"indexcount." + alpha.String(): "many"},
		{"indexabstract.short": "abc"},
	}
	for _, msg := range cases {
		if _, err := yagoproto.ParseSearchResponse(msg); err == nil {
			t.Fatalf("ParseSearchResponse(%v) should fail", msg)
		}
	}
}

func TestParseSearchResponseSkipsMissingAndBadResources(t *testing.T) {
	t.Parallel()

	valid := sampleURLRow(t, "url-a")
	msg := yagomodel.Message{
		yagoproto.FieldCount: "3",
		"resource0":          valid.String(),
		"resource2":          "bad",
	}
	got, err := yagoproto.ParseSearchResponse(msg)
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
	msg := yagomodel.Message{
		"resource0": valid.String(),
		"resource1": "bad",
	}
	got, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	if len(got.Resources) != 1 || !reflect.DeepEqual(got.Resources[0], valid) {
		t.Fatalf("resources = %+v", got.Resources)
	}
}

func TestParseSearchResponseBoundsDeclaredResourceCount(t *testing.T) {
	t.Parallel()

	first := sampleURLRow(t, "url-a")
	beyondLimit := sampleURLRow(t, "url-b")
	msg := yagomodel.Message{
		yagoproto.FieldCount: strconv.Itoa(math.MaxInt),
		"resource0":          first.String(),
		"resource1024":       beyondLimit.String(),
	}
	got, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	if got.Count != math.MaxInt {
		t.Fatalf("count = %d, want %d", got.Count, math.MaxInt)
	}
	if len(got.Resources) != 1 || !reflect.DeepEqual(got.Resources[0], first) {
		t.Fatalf("resources = %+v", got.Resources)
	}
}

func TestParseSearchResponsePreservesSparseDeclaredResources(t *testing.T) {
	t.Parallel()

	first := sampleURLRow(t, "url-a")
	last := sampleURLRow(t, "url-b")
	beyondCount := sampleURLRow(t, "url-c")
	msg := yagomodel.Message{
		yagoproto.FieldCount: "5",
		"resource0":          first.String(),
		"resource4":          last.String(),
		"resource5":          beyondCount.String(),
	}
	got, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	want := []yagomodel.URIMetadataRow{first, last}
	if !reflect.DeepEqual(got.Resources, want) {
		t.Fatalf("resources = %+v, want %+v", got.Resources, want)
	}
}
