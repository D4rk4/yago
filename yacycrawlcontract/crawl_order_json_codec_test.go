package yacycrawlcontract

import (
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

func TestCrawlOrderRoundTrip(t *testing.T) {
	order := CrawlOrder{
		Provenance: []byte{0x00, 0x01, 0xff, 0x7f, 0x80},
		Profile: NewCrawlProfile(CrawlProfile{
			Name:                "deep",
			Scope:               ScopeSubpath,
			URLMustMatch:        MatchAll,
			URLMustNotMatch:     ".*\\.pdf",
			MaxDepth:            4,
			AllowQueryURLs:      true,
			FollowNoFollowLinks: true,
			MaxPagesPerHost:     100,
			RecrawlIfOlder:      24 * time.Hour,
			CrawlDelay:          2 * time.Second,
		}),
		Requests: []CrawlRequest{
			{
				URL:           "https://example.org/a",
				Mode:          CrawlRequestModeSitemap,
				ReferrerURL:   "https://example.org/",
				AnchorName:    "link",
				Depth:         1,
				ProfileHandle: "abcdef012345",
				Initiator:     yacymodel.Hash("0123456789AB"),
				AppDate:       time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
				LastModified:  time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	data, err := MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCrawlOrder(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(order, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", order, got)
	}
}

func TestUnmarshalCrawlOrderRejectsInvalidJSON(t *testing.T) {
	if _, err := UnmarshalCrawlOrder([]byte("{")); err == nil {
		t.Fatal("invalid JSON should fail")
	}
}

func TestNormalizeCrawlRequestMode(t *testing.T) {
	cases := map[CrawlRequestMode]CrawlRequestMode{
		"":                       CrawlRequestModeURL,
		CrawlRequestModeURL:      CrawlRequestModeURL,
		CrawlRequestModeSitemap:  CrawlRequestModeSitemap,
		CrawlRequestModeSitelist: CrawlRequestModeSitelist,
	}
	for input, want := range cases {
		got, ok := NormalizeCrawlRequestMode(input)
		if !ok || got != want {
			t.Fatalf("mode %q = %q/%v, want %q/true", input, got, ok, want)
		}
	}
	if _, ok := NormalizeCrawlRequestMode("archive"); ok {
		t.Fatal("unknown mode should be rejected")
	}
}
