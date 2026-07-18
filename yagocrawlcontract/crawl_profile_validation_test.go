package yagocrawlcontract_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func validatableProfile() yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.CrawlProfile{
		Name:            "news",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        3,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	}
}

func TestValidateAcceptsSaneProfile(t *testing.T) {
	profile := validatableProfile()
	profile.URLMustMatch = `https://example\.org/.*`
	profile.URLMustNotMatch = `\.pdf$`
	profile.MaxPagesPerHost = 100
	profile.RecrawlIfOlder = 0
	profile.CrawlDelay = 0
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateAcceptsBoundaryDepth(t *testing.T) {
	profile := validatableProfile()
	profile.MaxDepth = yagocrawlcontract.MaxCrawlDepth
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() at max depth = %v, want nil", err)
	}
	profile.MaxDepth = 0
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() at zero depth = %v, want nil", err)
	}
}

func TestValidateRejectsDangerousDefaults(t *testing.T) {
	cases := map[string]struct {
		mutate func(*yagocrawlcontract.CrawlProfile)
		want   string
	}{
		"negative depth": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.MaxDepth = -1 },
			want:   "maxDepth must not be negative",
		},
		"unbounded depth": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) {
				p.MaxDepth = yagocrawlcontract.MaxCrawlDepth + 1
			},
			want: "maxDepth must not exceed",
		},
		"zero pages per host": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.MaxPagesPerHost = 0 },
			want:   "maxPagesPerHost must be positive",
		},
		"negative pages per host": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.MaxPagesPerHost = -2 },
			want:   "maxPagesPerHost must be positive",
		},
		"negative pages per run": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) {
				value := -1
				p.MaxPagesPerRun = &value
			},
			want: "maxPagesPerRun must not be negative",
		},
		"negative recrawl": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.RecrawlIfOlder = -1 },
			want:   "recrawlIfOlder must not be negative",
		},
		"negative delay": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.CrawlDelay = -1 },
			want:   "crawlDelay must not be negative",
		},
		"impossible must-match": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.URLMustMatch = "(" },
			want:   "urlMustMatch is not a valid regular expression",
		},
		"impossible must-not-match": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.URLMustNotMatch = "[" },
			want:   "urlMustNotMatch is not a valid regular expression",
		},
		"impossible index must-match": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.IndexURLMustMatch = "(" },
			want:   "indexMustMatch is not a valid regular expression",
		},
		"impossible index must-not-match": {
			mutate: func(p *yagocrawlcontract.CrawlProfile) { p.IndexURLMustNotMatch = "[" },
			want:   "indexMustNotMatch is not a valid regular expression",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			profile := validatableProfile()
			tc.mutate(&profile)
			err := profile.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateAcceptsExplicitUnlimitedPagesPerRun(t *testing.T) {
	profile := validatableProfile()
	unlimited := 0
	profile.MaxPagesPerRun = &unlimited
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateAcceptsEmptyAndMatchAllPatterns(t *testing.T) {
	profile := validatableProfile()
	profile.URLMustMatch = ""
	profile.URLMustNotMatch = yagocrawlcontract.MatchAll
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
