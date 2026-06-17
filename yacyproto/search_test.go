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
		Count:      10,
		Time:       3000,
		MaxDist:    5,
		Partitions: 30,
		Language:   "en",
		Author:     "ada",
		Protocol:   "https",
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

func TestParseSearchRequestRejectsRaggedQuery(t *testing.T) {
	t.Parallel()

	form := url.Values{yacyproto.FieldQuery: {"tooshort"}}
	if _, err := yacyproto.ParseSearchRequest(form); err == nil {
		t.Fatal("expected error for query not a multiple of the hash length")
	}
}
