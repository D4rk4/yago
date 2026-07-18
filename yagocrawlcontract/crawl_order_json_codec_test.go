package yagocrawlcontract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestCrawlOrderRoundTrip(t *testing.T) {
	maxPagesPerRun := 800
	order := CrawlOrder{
		Provenance: []byte{0x00, 0x01, 0xff, 0x7f, 0x80},
		Priority:   CrawlOrderPriorityAutomaticDiscovery,
		Profile: NewCrawlProfile(CrawlProfile{
			Name:                     "deep",
			Scope:                    ScopeSubpath,
			URLMustMatch:             MatchAll,
			URLMustNotMatch:          ".*\\.pdf",
			MaxDepth:                 4,
			AllowQueryURLs:           true,
			FollowNoFollowLinks:      true,
			NoindexCanonicalMismatch: true,
			MaxPagesPerHost:          100,
			MaxPagesPerRun:           &maxPagesPerRun,
			RecrawlIfOlder:           24 * time.Hour,
			CrawlDelay:               2 * time.Second,
		}),
		Requests: []CrawlRequest{
			{
				URL:           "https://example.org/a",
				Mode:          CrawlRequestModeSitemap,
				ReferrerURL:   "https://example.org/",
				AnchorName:    "link",
				Depth:         1,
				ProfileHandle: "abcdef012345",
				Initiator:     yagomodel.Hash("0123456789AB"),
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

func TestCrawlOrderPageBudgetCompatibility(t *testing.T) {
	legacy, err := UnmarshalCrawlOrder([]byte(`{"Profile":{"MaxPagesPerHost":-1}}`))
	if err != nil {
		t.Fatalf("unmarshal legacy order: %v", err)
	}
	if legacy.Profile.MaxPagesPerRun != nil {
		t.Fatalf("legacy max pages per run = %v, want nil", legacy.Profile.MaxPagesPerRun)
	}
	if got := legacy.Profile.EffectiveMaxPagesPerRun(321); got != 321 {
		t.Fatalf("legacy effective max pages per run = %d, want 321", got)
	}
	encoded, err := MarshalCrawlOrder(legacy)
	if err != nil {
		t.Fatalf("marshal legacy order: %v", err)
	}
	if bytes.Contains(encoded, []byte("MaxPagesPerRun")) {
		t.Fatalf("legacy encoding unexpectedly contains MaxPagesPerRun: %s", encoded)
	}

	unlimited := 0
	current := CrawlOrder{Profile: CrawlProfile{MaxPagesPerRun: &unlimited}}
	encoded, err = MarshalCrawlOrder(current)
	if err != nil {
		t.Fatalf("marshal current order: %v", err)
	}
	decoded, err := UnmarshalCrawlOrder(encoded)
	if err != nil {
		t.Fatalf("unmarshal current order: %v", err)
	}
	if decoded.Profile.MaxPagesPerRun == nil || *decoded.Profile.MaxPagesPerRun != 0 {
		t.Fatalf(
			"current max pages per run = %v, want explicit zero",
			decoded.Profile.MaxPagesPerRun,
		)
	}
	var older struct {
		Profile struct {
			MaxPagesPerHost int
		}
	}
	if err := json.Unmarshal(encoded, &older); err != nil {
		t.Fatalf("older decoder rejected additive profile field: %v", err)
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
		CrawlRequestModeRobots:   CrawlRequestModeRobots,
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
