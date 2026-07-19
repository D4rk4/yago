package crawlorder

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlOrderConsumerClampsCompiledProfileDepth(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	).WithMaximumDepth(2)
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name: "bounded", MaxDepth: 9,
	})
	compiled, ok := consumer.compileCrawlOrder(t.Context(), yagocrawlcontract.CrawlOrder{
		Profile: profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			Mode: yagocrawlcontract.CrawlRequestModeURL,
			URL:  "https://example.com/",
		}},
	}, CrawlOrderDelivery{})
	if !ok {
		t.Fatal("crawl order did not compile")
	}
	if compiled.Profile.MaxDepth != 2 || compiled.Profile.Handle != profile.Handle {
		t.Fatalf("compiled profile = %+v", compiled.Profile)
	}
}

func TestDefaultMaximumDepthPreservesAutomaticDiscoveryProfile(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	).WithMaximumDepth(yagocrawlcontract.DefaultCrawlerMaximumDepth)
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name: "automatic discovery", MaxDepth: 5,
	})
	preparedProfile, requests, prepared := consumer.prepareCrawlOrder(
		t.Context(),
		yagocrawlcontract.CrawlOrder{
			Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
			Profile:  profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				Mode: yagocrawlcontract.CrawlRequestModeURL,
				URL:  "https://automatic.example/",
			}},
		},
		CrawlOrderDelivery{},
	)
	if !prepared || len(requests) != 1 {
		t.Fatalf("automatic order prepared/requests = %t/%d", prepared, len(requests))
	}
	if preparedProfile.Profile.MaxDepth != 5 || preparedProfile.Profile.Handle != profile.Handle {
		t.Fatalf("prepared automatic profile = %+v", preparedProfile.Profile)
	}
}
