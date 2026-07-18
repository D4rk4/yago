package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type effectiveDirectivesCase struct {
	name string
	job  crawljob.CrawlJob
	page pageparse.ParsedPage
	tag  string
	want pageDirectives
}

func effectiveDirectivesCases() []effectiveDirectivesCase {
	return []effectiveDirectivesCase{
		{
			name: "meta only",
			page: pageparse.ParsedPage{MetaNoindex: true, MetaNofollow: true},
			want: pageDirectives{
				noindex: true, nofollow: true,
				noindexSource: "meta", nofollowSource: "meta",
			},
		},
		{
			name: "header only",
			tag:  "none",
			want: pageDirectives{
				noindex: true, nofollow: true,
				noindexSource: "header", nofollowSource: "header",
			},
		},
		{
			name: "meta and header",
			page: pageparse.ParsedPage{MetaNoindex: true, MetaNofollow: true},
			tag:  "none",
			want: pageDirectives{
				noindex: true, nofollow: true,
				noindexSource: "meta+header", nofollowSource: "meta+header",
			},
		},
		{
			name: "neither",
			want: pageDirectives{},
		},
		{
			name: "ignore robots waives page directives",
			job:  crawljob.CrawlJob{IgnoreRobots: true},
			page: pageparse.ParsedPage{MetaNoindex: true, MetaNofollow: true},
			tag:  "none",
			want: pageDirectives{},
		},
		{
			name: "canonical mismatch",
			job:  crawljob.CrawlJob{NoindexCanonicalMismatch: true},
			page: pageparse.ParsedPage{
				URL:          "https://example.com/page",
				CanonicalURL: "https://example.com/other",
			},
			want: pageDirectives{noindex: true, noindexSource: "canonical"},
		},
		{
			name: "meta noindex outranks canonical source",
			job:  crawljob.CrawlJob{NoindexCanonicalMismatch: true},
			page: pageparse.ParsedPage{
				URL:          "https://example.com/page",
				CanonicalURL: "https://example.com/other",
				MetaNoindex:  true,
			},
			want: pageDirectives{noindex: true, noindexSource: "meta"},
		},
		{
			name: "canonical absent",
			job:  crawljob.CrawlJob{NoindexCanonicalMismatch: true},
			page: pageparse.ParsedPage{URL: "https://example.com/page"},
			want: pageDirectives{},
		},
		{
			name: "canonical on unparseable page url",
			job:  crawljob.CrawlJob{NoindexCanonicalMismatch: true},
			page: pageparse.ParsedPage{
				URL:          "ftp://example.com/page",
				CanonicalURL: "https://example.com/other",
			},
			want: pageDirectives{},
		},
		{
			name: "follow-nofollow keeps the source for the log",
			job:  crawljob.CrawlJob{FollowNoFollowLinks: true},
			page: pageparse.ParsedPage{MetaNofollow: true},
			want: pageDirectives{nofollowSource: "meta"},
		},
	}
}

func TestEffectiveDirectivesSources(t *testing.T) {
	for _, tc := range effectiveDirectivesCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveDirectives(tc.job, tc.page, tc.tag); got != tc.want {
				t.Errorf("effectiveDirectives = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRedirectAdmittedToleratesUnnormalizableURLs(t *testing.T) {
	p := &Pipeline{}
	ctx := context.Background()
	if !p.redirectAdmitted(ctx, crawljob.CrawlJob{URL: "https://example.com/"}, "ftp://x/") {
		t.Error("an unnormalizable final URL must admit the page")
	}
	if !p.redirectAdmitted(ctx, crawljob.CrawlJob{URL: "ftp://x/"}, "https://example.com/") {
		t.Error("an unnormalizable job URL must admit the page")
	}
}

func TestRedirectAdmittedRejectsOverlongIdentityURL(t *testing.T) {
	p := &Pipeline{}
	finalURL := "https://example.com/" + strings.Repeat(
		"x",
		yagocrawlcontract.MaximumCrawlURLBytes,
	)
	if p.redirectAdmitted(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/start"},
		finalURL,
	) {
		t.Fatal("overlong redirect target must not be indexed")
	}
}
