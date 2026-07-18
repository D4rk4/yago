package crawlseed_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlseed"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestExpanderDiscoversSitemapsFromRobots(t *testing.T) {
	source := seedSource{
		"https://example.org/robots.txt": {
			Body: []byte("User-agent: *\n" +
				"Disallow: /private\n" +
				"Sitemap: https://example.org/sitemap.xml\n" +
				"Sitemap: ftp://example.org/ignored.xml\n"),
		},
		"https://example.org/sitemap.xml": {
			Body: []byte(`<urlset><url><loc>/a</loc></url></urlset>`),
		},
	}
	req := yagocrawlcontract.CrawlRequest{
		URL:           "https://example.org/",
		Mode:          yagocrawlcontract.CrawlRequestModeRobots,
		ProfileHandle: "profile",
	}

	got, err := crawlseed.NewExpander(source, 10).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{req})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 ||
		got[0].URL != "https://example.org/a" ||
		got[0].Mode != yagocrawlcontract.CrawlRequestModeURL ||
		got[0].ReferrerURL != "https://example.org/sitemap.xml" ||
		got[0].ProfileHandle != "profile" {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderRobotsWithoutSitemapsIsEmpty(t *testing.T) {
	source := seedSource{
		"https://example.org/robots.txt": {
			Body: []byte("User-agent: *\nDisallow: /private\n"),
		},
	}

	got, err := crawlseed.NewExpander(source, 10).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/",
			Mode: yagocrawlcontract.CrawlRequestModeRobots,
		}})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("requests = %#v, want none", got)
	}
}

func TestExpanderRobotsMissingFileFailsOpen(t *testing.T) {
	got, err := crawlseed.NewExpander(failingSeedSource{
		err: &pagefetch.GoneError{Status: http.StatusNotFound},
	}, 10).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/",
			Mode: yagocrawlcontract.CrawlRequestModeRobots,
		}})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("requests = %#v, want none", got)
	}
}

func TestExpanderRobotsPropagatesRetryableFetchFailures(t *testing.T) {
	for _, failure := range []error{errors.New("network failed"), context.Canceled} {
		_, err := crawlseed.NewExpander(failingSeedSource{err: failure}, 10).
			Expand(context.Background(), []yagocrawlcontract.CrawlRequest{{
				URL:  "https://example.org/",
				Mode: yagocrawlcontract.CrawlRequestModeRobots,
			}})
		if !errors.Is(err, failure) {
			t.Fatalf("error = %v, want %v", err, failure)
		}
	}
}

func TestExpanderRobotsRequiresSource(t *testing.T) {
	_, err := crawlseed.NewExpander(nil, 10).Expand(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/",
			Mode: yagocrawlcontract.CrawlRequestModeRobots,
		}},
	)
	if err == nil {
		t.Fatal("nil source should error")
	}
}

func TestExpanderRobotsRejectsNonHTTPSeed(t *testing.T) {
	_, err := crawlseed.NewExpander(seedSource{}, 10).Expand(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{{
			URL:  "ftp://example.org/",
			Mode: yagocrawlcontract.CrawlRequestModeRobots,
		}},
	)
	if err == nil {
		t.Fatal("non-http seed should error")
	}
}
