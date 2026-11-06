package pipeline_test

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const feedDocumentURL = "https://example.org/feed.xml"

func feedWithItems(items string) []byte {
	return []byte(
		`<?xml version="1.0"?><rss version="2.0"><channel><title>Harbor news</title>` + items,
	)
}

func feedItem(link string) string {
	return `<item><title>Story</title><link>` + link + `</link></item>`
}

func acceptingEmitter() emitFunc {
	return func(
		context.Context,
		yagocrawlcontract.DocumentIngest,
		[]yagomodel.RWIPosting,
		yagomodel.URIMetadataRow,
		ingest.Envelope,
	) error {
		return nil
	}
}

func TestFeedPageReportsOutcomeAndSubmitsOnlyClosedEntries(t *testing.T) {
	cases := []struct {
		name       string
		body       []byte
		robotsTag  string
		wantFailed uint64
		wantLinks  []string
	}{
		{
			name: "truncated second item",
			body: feedWithItems(
				feedItem(
					"https://example.org/news/one",
				) + `<item><link>https://example.org/news/two</link>`,
			),
			wantFailed: 1,
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name:       "error before first closed item",
			body:       feedWithItems(`<item><link>https://example.org/news/one</link></channel>`),
			wantFailed: 1,
		},
		{
			name: "valid empty feed",
			body: feedWithItems(`</channel></rss>`),
		},
		{
			name:      "nofollow response header",
			body:      feedWithItems(feedItem("https://example.org/news/one") + `</channel></rss>`),
			robotsTag: "nofollow",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newRecordingFrontier()
			crawlerPipeline := pipeline.NewPipeline(
				recorder,
				fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
					return pagefetch.FetchedPage{
						URL:         target,
						HTTPStatus:  200,
						ContentType: "application/rss+xml",
						RobotsTag:   testCase.robotsTag,
						Body:        testCase.body,
					}, nil
				}),
				pageindex.NewIndexBuilder(),
				acceptingEmitter(),
			)
			done := runJob(t, crawlerPipeline, recorder, crawljob.CrawlJob{
				URL:           feedDocumentURL,
				ProfileHandle: "feed-profile",
				Index:         true,
				Formats:       yagocrawlcontract.FormatToggles{XMLFeeds: true},
			})
			if done.outcome.Failed != testCase.wantFailed {
				t.Fatalf("failed pages = %d, want %d", done.outcome.Failed, testCase.wantFailed)
			}
			var submitted []string
			for _, links := range recorder.submitted {
				submitted = append(submitted, links.Followable...)
			}
			if !slices.Equal(submitted, testCase.wantLinks) {
				t.Fatalf("submitted links = %q, want %q", submitted, testCase.wantLinks)
			}
		})
	}
}

func sameDomainFeedProfile(t *testing.T) crawladmission.AdmissionProfile {
	t.Helper()
	profile, err := crawladmission.CompileProfile(yagocrawlcontract.NewCrawlProfile(
		yagocrawlcontract.CrawlProfile{
			Scope:           yagocrawlcontract.ScopeDomain,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxDepth:        1,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
			Formats:         yagocrawlcontract.FormatToggles{Text: true, XMLFeeds: true},
		},
	))
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}

	return profile
}

func TestFeedLinksPassFrontierAdmission(t *testing.T) {
	pathSegments := make([]string, 25)
	for index := range pathSegments {
		pathSegments[index] = "part" + strconv.Itoa(index)
	}
	deepPath := "/" + strings.Join(pathSegments, "/")
	feed := feedWithItems(
		feedItem("https://example.org/news/one") +
			feedItem("https://example.org/news/one") +
			feedItem("/news/relative") +
			feedItem("https://example.test/elsewhere") +
			feedItem("https://example.org/search?q=harbor") +
			feedItem("https://example.org"+deepPath) +
			`<item><link>https://example.org/news/unfinished</link>`,
	)
	var fetchedMutex sync.Mutex
	var fetchedChildren []string
	crawlFrontier := frontier.NewFrontier(16, nil)
	crawlerPipeline := pipeline.NewPipeline(
		crawlFrontier,
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			if target.String() == feedDocumentURL {
				return pagefetch.FetchedPage{
					URL:         target,
					HTTPStatus:  200,
					ContentType: "application/rss+xml",
					Body:        feed,
				}, nil
			}
			fetchedMutex.Lock()
			fetchedChildren = append(fetchedChildren, target.String())
			fetchedMutex.Unlock()

			return pagefetch.FetchedPage{
				URL:         target,
				HTTPStatus:  200,
				ContentType: "text/plain",
				Body:        []byte("harbor story"),
			}, nil
		}),
		pageindex.NewIndexBuilder(),
		acceptingEmitter(),
	)
	profile := sameDomainFeedProfile(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	seeded := crawlFrontier.SeedRun(
		ctx,
		[]yagocrawlcontract.CrawlRequest{
			{URL: feedDocumentURL, ProfileHandle: profile.Profile.Handle},
		},
		nil,
		profile,
		func(bool) { close(finished) },
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued seeds = %d, want 1", seeded.Queued)
	}
	go crawlerPipeline.RunWorkers(ctx, ctx, 1)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("crawl run never finished")
	}
	fetchedMutex.Lock()
	defer fetchedMutex.Unlock()
	slices.Sort(fetchedChildren)
	want := []string{"https://example.org/news/one", "https://example.org/news/relative"}
	if !slices.Equal(fetchedChildren, want) {
		t.Fatalf("fetched feed links = %q, want %q", fetchedChildren, want)
	}
}
