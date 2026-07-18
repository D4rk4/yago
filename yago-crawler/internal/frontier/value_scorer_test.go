package frontier

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

func TestDefaultValueScorerOrdersBySignals(t *testing.T) {
	shallow := DefaultValueScorer(crawljob.CrawlJob{URL: "https://a.example/page", Depth: 0}, 0)
	deep := DefaultValueScorer(crawljob.CrawlJob{URL: "https://a.example/page", Depth: 5}, 0)
	if shallow <= deep {
		t.Fatalf("depth signal inverted: shallow %v <= deep %v", shallow, deep)
	}

	novel := DefaultValueScorer(crawljob.CrawlJob{URL: "https://b.example/page"}, 0)
	crowded := DefaultValueScorer(crawljob.CrawlJob{URL: "https://b.example/page"}, 40)
	if novel <= crowded {
		t.Fatalf("novelty signal inverted: novel %v <= crowded %v", novel, crowded)
	}

	clean := DefaultValueScorer(crawljob.CrawlJob{URL: "https://c.example/article"}, 0)
	tangled := DefaultValueScorer(crawljob.CrawlJob{
		URL: "https://c.example/a/b/c/d/e/list?page=9&sort=date&filter=old",
	}, 0)
	if clean <= tangled {
		t.Fatalf("url-shape signal inverted: clean %v <= tangled %v", clean, tangled)
	}

	broken := DefaultValueScorer(crawljob.CrawlJob{URL: "http://%zz", Depth: 1}, 1)
	if broken <= 0 {
		t.Fatalf("unparsable URL lost its base score: %v", broken)
	}
}

func TestWithValueScorerReplacesHeuristicAndIgnoresNil(t *testing.T) {
	frontier := NewFrontier(1, nil, WithValueScorer(nil))
	if frontier.scorer == nil {
		t.Fatal("nil scorer displaced the default")
	}
	custom := func(crawljob.CrawlJob, int) float64 { return 42 }
	frontier = NewFrontier(1, nil, WithValueScorer(custom))
	if got := frontier.scorer(crawljob.CrawlJob{}, 0); got != 42 {
		t.Fatalf("custom scorer not installed: %v", got)
	}
}
