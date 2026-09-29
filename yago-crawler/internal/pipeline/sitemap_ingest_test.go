package pipeline_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func TestPipelineFollowsSitemapURLsWithoutIngestingTheSitemap(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		limit        int
		want         []string
		ignoreRobots bool
	}{
		{
			name: "url set",
			body: "\ufeff<?xml version=\"1.0\" encoding=\"UTF-8\"?><!-- sitemap -->" +
				`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<url><loc>https://alpha.example.test/one</loc></url>` +
				`<url><loc>https://alpha.example.test/two</loc></url></urlset>`,
			limit: 2,
			want: []string{
				"https://alpha.example.test/one",
				"https://alpha.example.test/two",
			},
			ignoreRobots: true,
		},
		{
			name: "sitemap index obeys the configured limit",
			body: `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<sitemap><loc>https://alpha.example.test/map-a.xml</loc></sitemap>` +
				`<sitemap><loc>https://alpha.example.test/map-b.xml</loc></sitemap></sitemapindex>`,
			limit: 1,
			want:  []string{"https://alpha.example.test/map-a.xml"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const target = "https://alpha.example.test/sitemap.xml"
			frontier := newRecordingFrontier()
			emitted := 0
			fetchedURL, err := url.Parse(target)
			if err != nil {
				t.Fatal(err)
			}
			crawlerPipeline := pipeline.NewPipeline(
				frontier,
				fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
					return pagefetch.FetchedPage{
						URL:         fetchedURL,
						HTTPStatus:  200,
						ContentType: "application/xml",
						Body:        []byte(tc.body),
					}, nil
				}),
				pageindex.NewIndexBuilder(),
				emitFunc(func(
					context.Context,
					yagocrawlcontract.DocumentIngest,
					[]yagomodel.RWIPosting,
					yagomodel.URIMetadataRow,
					ingest.Envelope,
				) error {
					emitted++
					return nil
				}),
				pipeline.WithSitemapURLLimit(tc.limit),
			)
			done := runJob(t, crawlerPipeline, frontier, crawljob.CrawlJob{
				URL: target, ProfileHandle: "profile", Index: true,
				IgnoreRobots: tc.ignoreRobots,
				Formats:      yagocrawlcontract.DefaultFormatToggles(),
			})
			assertSitemapJobOutcome(t, frontier, done, emitted, tc.want)
		})
	}
}

func assertSitemapJobOutcome(
	t *testing.T,
	frontier *recordingFrontier,
	done doneCall,
	emitted int,
	want []string,
) {
	t.Helper()
	if emitted != 0 {
		t.Errorf("sitemap ingest batches = %d, want none", emitted)
	}
	if len(frontier.submitted) != 1 ||
		len(frontier.submitted[0].Followable) != len(want) {
		t.Fatalf("submitted sitemap links = %v, want %v", frontier.submitted, want)
	}
	for index, link := range want {
		if frontier.submitted[0].Followable[index] != link {
			t.Fatalf(
				"followable sitemap links = %v, want %v",
				frontier.submitted[0].Followable,
				want,
			)
		}
	}
	if done.outcome.Fetched != 1 || done.outcome.Indexed != 0 || done.outcome.Failed != 0 {
		t.Fatalf("sitemap outcome = %+v, want fetched 1, indexed 0, failed 0", done.outcome)
	}
	if done.reason != "sitemap used for URL discovery only" {
		t.Fatalf("sitemap outcome reason = %q", done.reason)
	}
}
