package pipeline_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagomodel"
)

func countingFetcher(hits *int) pagefetch.PageSource {
	return fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
		*hits++

		return htmlPage(), nil
	})
}

// TestPipelineRoutesRobotsOptOutJobsThroughDirectChains proves the CRAWL-04
// matrix: IgnoreRobots picks the robots-skipping variant of whichever TLS
// chain the job already uses.
func TestPipelineRoutesRobotsOptOutJobsThroughDirectChains(t *testing.T) {
	frontier := newRecordingFrontier()
	var standard, insecure, direct, insecureDirect int
	p := pipeline.NewPipeline(
		frontier,
		countingFetcher(&standard),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
		pipeline.WithInsecureFetcher(countingFetcher(&insecure)),
		pipeline.WithRobotsIgnoringFetchers(
			countingFetcher(&direct),
			countingFetcher(&insecureDirect),
		),
	)

	frontier.jobs = make(chan crawljob.CrawlJob, 4)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/a"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/b", IgnoreRobots: true}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/c", IgnoreTLSAuthority: true}
	frontier.jobs <- crawljob.CrawlJob{
		URL:                "https://example.com/d",
		IgnoreRobots:       true,
		IgnoreTLSAuthority: true,
	}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if standard != 1 || insecure != 1 || direct != 1 || insecureDirect != 1 {
		t.Fatalf(
			"chain hits = standard %d insecure %d direct %d insecureDirect %d, want 1 each",
			standard, insecure, direct, insecureDirect,
		)
	}
}

// TestPipelineKeepsRobotsOptOutOnSafeChainsWhenUnwired: an unwired
// robots-skipping variant falls back to the robots-obeying chain.
func TestPipelineKeepsRobotsOptOutOnSafeChainsWhenUnwired(t *testing.T) {
	frontier := newRecordingFrontier()
	var standard, insecure int
	p := pipeline.NewPipeline(
		frontier,
		countingFetcher(&standard),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
		pipeline.WithInsecureFetcher(countingFetcher(&insecure)),
		pipeline.WithRobotsIgnoringFetchers(nil, nil),
	)

	frontier.jobs = make(chan crawljob.CrawlJob, 2)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/a", IgnoreRobots: true}
	frontier.jobs <- crawljob.CrawlJob{
		URL:                "https://example.com/b",
		IgnoreRobots:       true,
		IgnoreTLSAuthority: true,
	}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if standard != 1 || insecure != 1 {
		t.Fatalf("fallback hits = standard %d insecure %d, want 1 each", standard, insecure)
	}
}
