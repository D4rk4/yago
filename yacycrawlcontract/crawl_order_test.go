package yacycrawlcontract

import (
	"reflect"
	"testing"
	"time"
)

func TestCrawlOrderRoundTrip(t *testing.T) {
	order := CrawlOrder{
		OrderID: "3f8a2c14-6b2d-4e1a-9c7f-8d0e1a2b3c4d",
		Profile: NewCrawlProfile(CrawlProfile{
			Name:            "deep",
			Scope:           ScopeSubpath,
			URLMustMatch:    MatchAll,
			URLMustNotMatch: ".*\\.pdf",
			MaxDepth:        4,
			AllowQueryURLs:  true,
			MaxPagesPerHost: 100,
			CrawlDelay:      2 * time.Second,
		}),
		SeedURLs: []string{"https://example.org/a", "https://example.org/b"},
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
