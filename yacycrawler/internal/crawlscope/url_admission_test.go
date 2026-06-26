package crawlscope_test

import (
	"slices"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlscope"
)

func TestCompileProfileRejectsBadRegex(t *testing.T) {
	if _, err := crawlscope.CompileProfile(
		yacycrawlcontract.CrawlProfile{URLMustMatch: "("},
	); err == nil {
		t.Error("expected error for bad URLMustMatch")
	}
	if _, err := crawlscope.CompileProfile(
		yacycrawlcontract.CrawlProfile{URLMustNotMatch: "("},
	); err == nil {
		t.Error("expected error for bad URLMustNotMatch")
	}
}

func TestURLAllowed(t *testing.T) {
	compiled, err := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		URLMustMatch:    `example\.com`,
		URLMustNotMatch: `/private`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.URLAllowed("https://example.com/page") {
		t.Error("expected allowed")
	}
	if compiled.URLAllowed("https://other.com/page") {
		t.Error("expected rejected by mustMatch")
	}
	if compiled.URLAllowed("https://example.com/private") {
		t.Error("expected rejected by mustNotMatch")
	}
}

func TestURLAllowedMatchAllSkipsRegex(t *testing.T) {
	compiled, err := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		URLMustMatch: yacycrawlcontract.MatchAll,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.URLAllowed("https://anything.test/x") {
		t.Error("MatchAll should allow any url")
	}
}

func TestAdmitLinksScope(t *testing.T) {
	domain, _ := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		Scope:        yacycrawlcontract.ScopeDomain,
		URLMustMatch: yacycrawlcontract.MatchAll,
	})
	got := domain.AdmitLinks("https://example.com/dir/page", []string{
		"https://example.com/other",
		"https://elsewhere.com/x",
		"/root",
		"ftp://example.com/skip",
		"mailto:a@b.com",
	})
	want := []string{"https://example.com/other", "https://example.com/root"}
	if !slices.Equal(got, want) {
		t.Errorf("domain admit = %v want %v", got, want)
	}
}

func TestAdmitLinksSubpath(t *testing.T) {
	sub, _ := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		Scope:        yacycrawlcontract.ScopeSubpath,
		URLMustMatch: yacycrawlcontract.MatchAll,
	})
	got := sub.AdmitLinks("https://example.com/dir/page", []string{
		"https://example.com/dir/child",
		"https://example.com/elsewhere",
	})
	want := []string{"https://example.com/dir/child"}
	if !slices.Equal(got, want) {
		t.Errorf("subpath admit = %v want %v", got, want)
	}
}

func TestAdmitLinksWideAndQuery(t *testing.T) {
	wide, _ := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		Scope:        yacycrawlcontract.ScopeWide,
		URLMustMatch: yacycrawlcontract.MatchAll,
	})
	got := wide.AdmitLinks("https://example.com/", []string{
		"https://elsewhere.com/x",
		"https://elsewhere.com/y?a=1",
	})
	want := []string{"https://elsewhere.com/x"}
	if !slices.Equal(got, want) {
		t.Errorf("wide admit (query filtered) = %v want %v", got, want)
	}

	allowQuery, _ := crawlscope.CompileProfile(yacycrawlcontract.CrawlProfile{
		Scope:          yacycrawlcontract.ScopeWide,
		URLMustMatch:   yacycrawlcontract.MatchAll,
		AllowQueryURLs: true,
	})
	got = allowQuery.AdmitLinks("https://example.com/", []string{"https://elsewhere.com/y?a=1"})
	want = []string{"https://elsewhere.com/y?a=1"}
	if !slices.Equal(got, want) {
		t.Errorf("allow-query admit = %v want %v", got, want)
	}
}

func TestAdmitLinksBadBaseURL(t *testing.T) {
	c, _ := crawlscope.CompileProfile(
		yacycrawlcontract.CrawlProfile{URLMustMatch: yacycrawlcontract.MatchAll},
	)
	if got := c.AdmitLinks("://bad", []string{"https://example.com/"}); got != nil {
		t.Errorf("bad base should admit nothing, got %v", got)
	}
}
