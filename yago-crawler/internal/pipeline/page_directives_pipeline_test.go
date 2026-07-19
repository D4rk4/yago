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

type directivesCase struct {
	name        string
	head        string
	robotsTag   string
	job         crawljob.CrawlJob
	wantEmit    bool
	wantSubmits int
}

func directivesJob(mutate func(*crawljob.CrawlJob)) crawljob.CrawlJob {
	job := crawljob.CrawlJob{URL: "https://example.com/", ProfileHandle: "h", Index: true}
	if mutate != nil {
		mutate(&job)
	}

	return job
}

func directivesCases() []directivesCase {
	metaNoindex := `<meta name="robots" content="noindex">`
	metaNofollow := `<meta name="robots" content="nofollow">`
	canonicalOther := `<link rel="canonical" href="https://example.com/canonical">`
	canonicalSelf := `<link rel="canonical" href="https://example.com/">`
	withCanonicalFlag := func(j *crawljob.CrawlJob) { j.NoindexCanonicalMismatch = true }

	return []directivesCase{
		{"meta noindex keeps links", metaNoindex, "", directivesJob(nil), false, 1},
		{"header noindex keeps links", "", "noindex", directivesJob(nil), false, 1},
		{"meta nofollow keeps index", metaNofollow, "", directivesJob(nil), true, 0},
		{"header nofollow keeps index", "", "nofollow", directivesJob(nil), true, 0},
		{
			"follow-nofollow overrides page nofollow", metaNofollow, "",
			directivesJob(func(j *crawljob.CrawlJob) { j.FollowNoFollowLinks = true }), true, 1,
		},
		{
			"ignore robots waives both", `<meta name="robots" content="none">`, "",
			directivesJob(func(j *crawljob.CrawlJob) { j.IgnoreRobots = true }), true, 1,
		},
		{"noindex and nofollow combine", metaNoindex, "nofollow", directivesJob(nil), false, 0},
		{
			"canonical mismatch with flag", canonicalOther, "",
			directivesJob(withCanonicalFlag), false, 1,
		},
		{"canonical mismatch without flag", canonicalOther, "", directivesJob(nil), true, 1},
		{
			"canonical self with flag", canonicalSelf, "",
			directivesJob(withCanonicalFlag), true, 1,
		},
	}
}

func directivesPipeline(
	tc directivesCase,
	emitted *int,
) (*recordingFrontier, *pipeline.Pipeline) {
	frontier := newRecordingFrontier()
	body := []byte(
		`<html><head>` + tc.head +
			`</head><body><a href="/next">go</a> words here</body></html>`,
	)
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			target, _ := url.Parse("https://example.com/")

			return pagefetch.FetchedPage{
				URL:         target,
				ContentType: "text/html",
				Body:        body,
				RobotsTag:   tc.robotsTag,
			}, nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				*emitted++

				return nil
			},
		),
	)

	return frontier, p
}

func TestPipelineHonorsPageRobotsDirectives(t *testing.T) {
	for _, tc := range directivesCases() {
		t.Run(tc.name, func(t *testing.T) {
			emitted := 0
			frontier, p := directivesPipeline(tc, &emitted)
			done := runJob(t, p, frontier, tc.job)
			if got := emitted > 0; got != tc.wantEmit {
				t.Errorf("emitted = %v, want %v", got, tc.wantEmit)
			}
			if !tc.wantEmit && done.reason != "page directives disabled indexing" {
				t.Errorf("noindex reason = %q", done.reason)
			}
			if len(frontier.submitted) != tc.wantSubmits {
				t.Errorf("submitted = %d sets, want %d", len(frontier.submitted), tc.wantSubmits)
			}
		})
	}
}
