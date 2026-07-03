package yacycrawlcontract_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

func validatableProfile() yacycrawlcontract.CrawlProfile {
	return yacycrawlcontract.CrawlProfile{
		Name:            "news",
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        3,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
	profile.MaxDepth = yacycrawlcontract.MaxCrawlDepth
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
		mutate func(*yacycrawlcontract.CrawlProfile)
		want   string
	}{
		"negative depth": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.MaxDepth = -1 },
			want:   "maxDepth must not be negative",
		},
		"unbounded depth": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) {
				p.MaxDepth = yacycrawlcontract.MaxCrawlDepth + 1
			},
			want: "maxDepth must not exceed",
		},
		"zero pages per host": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.MaxPagesPerHost = 0 },
			want:   "maxPagesPerHost must be positive",
		},
		"negative pages per host": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.MaxPagesPerHost = -2 },
			want:   "maxPagesPerHost must be positive",
		},
		"negative recrawl": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.RecrawlIfOlder = -1 },
			want:   "recrawlIfOlder must not be negative",
		},
		"negative delay": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.CrawlDelay = -1 },
			want:   "crawlDelay must not be negative",
		},
		"impossible must-match": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.URLMustMatch = "(" },
			want:   "urlMustMatch is not a valid regular expression",
		},
		"impossible must-not-match": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.URLMustNotMatch = "[" },
			want:   "urlMustNotMatch is not a valid regular expression",
		},
		"impossible index must-match": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.IndexURLMustMatch = "(" },
			want:   "indexMustMatch is not a valid regular expression",
		},
		"impossible index must-not-match": {
			mutate: func(p *yacycrawlcontract.CrawlProfile) { p.IndexURLMustNotMatch = "[" },
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

func TestValidateAcceptsEmptyAndMatchAllPatterns(t *testing.T) {
	profile := validatableProfile()
	profile.URLMustMatch = ""
	profile.URLMustNotMatch = yacycrawlcontract.MatchAll
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
