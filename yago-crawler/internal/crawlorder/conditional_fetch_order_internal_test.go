package crawlorder

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func issuedConditionalFetch() *yagocrawlcontract.ConditionalFetch {
	return &yagocrawlcontract.ConditionalFetch{
		Validators: yagocrawlcontract.HTTPValidators{
			ETag:         `W/"v4"`,
			LastModified: "Sat, 31 Oct 2026 05:00:00 GMT",
		},
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 4,
			FetchedAt:            time.Date(2026, 10, 31, 5, 0, 0, 0, time.UTC),
		},
	}
}

func TestAcceptTermsOrderWithInvalidConditionalFetch(t *testing.T) {
	injected := issuedConditionalFetch()
	injected.Validators.ETag = "\"v4\"\r\nSet-Cookie: x=1"
	withoutRevision := issuedConditionalFetch()
	withoutRevision.Revision = yagocrawlcontract.DocumentRevision{}
	cases := map[string]yagocrawlcontract.CrawlRequest{
		"sitemap request": {
			URL:              "https://example.org/sitemap.xml",
			Mode:             yagocrawlcontract.CrawlRequestModeSitemap,
			ConditionalFetch: issuedConditionalFetch(),
		},
		"injected entity tag": {
			URL:              "https://example.org/",
			ConditionalFetch: injected,
		},
		"absent stored document": {
			URL:              "https://example.org/",
			ConditionalFetch: withoutRevision,
		},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			consumer := NewCrawlOrderConsumer(
				boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
				frontier.NewFrontier(1, nil),
				unexpectedExpander{},
			)
			termed := make(chan struct{})
			consumer.accept(t.Context(), CrawlOrderDelivery{
				Order: yagocrawlcontract.CrawlOrder{
					Profile:  consumerProfile(),
					Requests: []yagocrawlcontract.CrawlRequest{request},
				},
				Nak: func(context.Context) error {
					t.Error("invalid conditional order must not be requeued")

					return nil
				},
				Term: func(context.Context) error {
					close(termed)

					return nil
				},
			})
			waitCallback(t, termed)
		})
	}
}

func TestAcceptSeedsConditionalFetchOnlyOnItsRequest(t *testing.T) {
	work := frontier.NewFrontier(2, nil)
	profile := consumerProfile()
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		work,
	)
	consumer.accept(t.Context(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{
			Profile: profile,
			Requests: []yagocrawlcontract.CrawlRequest{
				{
					URL:              "https://example.org/cached",
					ProfileHandle:    profile.Handle,
					ConditionalFetch: issuedConditionalFetch(),
				},
				{URL: "https://example.org/uncached", ProfileHandle: profile.Handle},
			},
		},
		Ack: func(context.Context) error { return nil },
		Nak: func(context.Context) error {
			t.Error("valid conditional order was requeued")

			return nil
		},
	})
	jobs := map[string]crawljob.CrawlJob{}
	for range 2 {
		job, ok := work.Take(t.Context())
		if !ok {
			t.Fatal("frontier closed before seeded jobs")
		}
		jobs[job.URL] = job
	}
	cached := jobs["https://example.org/cached"].ConditionalFetch
	if cached == nil || cached.Validators != issuedConditionalFetch().Validators ||
		!cached.Revision.Matches(issuedConditionalFetch().Revision) {
		t.Fatalf("cached job conditional fetch = %+v", cached)
	}
	if uncached := jobs["https://example.org/uncached"].ConditionalFetch; uncached != nil {
		t.Fatalf("uncached job conditional fetch = %+v, want none", uncached)
	}
}
