package crawladmission_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCompileProfileRejectsBadRegex(t *testing.T) {
	if _, err := crawladmission.CompileProfile(
		yagocrawlcontract.CrawlProfile{URLMustMatch: "("},
	); err == nil {
		t.Error("expected error for bad URLMustMatch")
	}
	if _, err := crawladmission.CompileProfile(
		yagocrawlcontract.CrawlProfile{URLMustNotMatch: "("},
	); err == nil {
		t.Error("expected error for bad URLMustNotMatch")
	}
	if _, err := crawladmission.CompileProfile(
		yagocrawlcontract.CrawlProfile{IndexURLMustMatch: "("},
	); err == nil {
		t.Error("expected error for bad IndexURLMustMatch")
	}
	if _, err := crawladmission.CompileProfile(
		yagocrawlcontract.CrawlProfile{IndexURLMustNotMatch: "("},
	); err == nil {
		t.Error("expected error for bad IndexURLMustNotMatch")
	}
}

func TestCompileProfileRejectsNegativeRunBudget(t *testing.T) {
	value := -1
	_, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		MaxPagesPerRun: &value,
	})
	if err == nil {
		t.Fatal("negative run budget accepted")
	}
}

func TestIndexAllowed(t *testing.T) {
	compiled, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		IndexURLMustMatch:    `/articles/`,
		IndexURLMustNotMatch: `/draft`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.IndexAllowed("https://example.com/articles/1") {
		t.Error("expected indexable")
	}
	if compiled.IndexAllowed("https://example.com/about") {
		t.Error("expected rejected by indexMustMatch")
	}
	if compiled.IndexAllowed("https://example.com/articles/draft") {
		t.Error("expected rejected by indexMustNotMatch")
	}
}

func TestIndexAllowedDefaultsToAll(t *testing.T) {
	compiled, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		IndexURLMustMatch: yagocrawlcontract.MatchAll,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.IndexAllowed("https://anything.test/x") {
		t.Error("default index rules should allow any url")
	}
}

func TestURLAllowed(t *testing.T) {
	compiled, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
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
	compiled, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		URLMustMatch: yagocrawlcontract.MatchAll,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.URLAllowed("https://anything.test/x") {
		t.Error("MatchAll should allow any url")
	}
}

func TestAdmitLinksScope(t *testing.T) {
	domain, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeDomain,
		URLMustMatch: yagocrawlcontract.MatchAll,
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

func TestAdmitLinksDomainScopeUnifiesWWW(t *testing.T) {
	domain, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeDomain,
		URLMustMatch: yagocrawlcontract.MatchAll,
	})

	fromBare := domain.AdmitLinks("https://anticisco.ru/", []string{
		"https://www.anticisco.ru/page",
		"https://blog.anticisco.ru/page",
		"https://other.ru/page",
	})
	if want := []string{"https://www.anticisco.ru/page"}; !slices.Equal(fromBare, want) {
		t.Errorf("bare-domain admit = %v want %v", fromBare, want)
	}

	fromWWW := domain.AdmitLinks("https://www.anticisco.ru/", []string{
		"https://anticisco.ru/page",
		"https://deep.www.anticisco.ru/page",
	})
	if want := []string{"https://anticisco.ru/page"}; !slices.Equal(fromWWW, want) {
		t.Errorf("www-domain admit = %v want %v", fromWWW, want)
	}
}

func TestAdmitLinksSubpathKeepsWWWDistinct(t *testing.T) {
	sub, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeSubpath,
		URLMustMatch: yagocrawlcontract.MatchAll,
	})

	got := sub.AdmitLinks("https://anticisco.ru/dir/page", []string{
		"https://anticisco.ru/dir/child",
		"https://www.anticisco.ru/dir/child",
	})
	want := []string{"https://anticisco.ru/dir/child"}
	if !slices.Equal(got, want) {
		t.Errorf("subpath must keep www distinct, admit = %v want %v", got, want)
	}
}

func TestAdmitLinksSubpath(t *testing.T) {
	sub, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeSubpath,
		URLMustMatch: yagocrawlcontract.MatchAll,
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

func TestAdmitLinksSubpathWithoutSlash(t *testing.T) {
	sub, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeSubpath,
		URLMustMatch: yagocrawlcontract.MatchAll,
	})
	got := sub.AdmitLinks("https://example.com", []string{
		"https://example.com/path",
		"https://elsewhere.com/path",
	})
	want := []string{"https://example.com/path"}
	if !slices.Equal(got, want) {
		t.Errorf("subpath root admit = %v want %v", got, want)
	}
}

func TestAdmitLinksWideAndQuery(t *testing.T) {
	wide, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeWide,
		URLMustMatch: yagocrawlcontract.MatchAll,
	})
	got := wide.AdmitLinks("https://example.com/", []string{
		"https://elsewhere.com/x",
		"https://elsewhere.com/y?a=1",
	})
	want := []string{"https://elsewhere.com/x"}
	if !slices.Equal(got, want) {
		t.Errorf("wide admit (query filtered) = %v want %v", got, want)
	}

	allowQuery, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:          yagocrawlcontract.ScopeWide,
		URLMustMatch:   yagocrawlcontract.MatchAll,
		AllowQueryURLs: true,
	})
	got = allowQuery.AdmitLinks("https://example.com/", []string{"https://elsewhere.com/y?a=1"})
	want = []string{"https://elsewhere.com/y?a=1"}
	if !slices.Equal(got, want) {
		t.Errorf("allow-query admit = %v want %v", got, want)
	}
}

func TestAdmitLinksRejectsURLPatternMismatch(t *testing.T) {
	compiled, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeWide,
		URLMustMatch: `allowed\.example`,
	})

	got := compiled.AdmitLinks("https://source.example/", []string{
		"https://allowed.example/page",
		"https://blocked.example/page",
	})
	want := []string{"https://allowed.example/page"}
	if !slices.Equal(got, want) {
		t.Errorf("pattern admit = %v want %v", got, want)
	}
}

// TestAdmitLinksDropsStructuralTraps pins that a discovered link whose structure
// smells like a crawler trap (here a path-recursion loop) is refused before it
// can enter the frontier, while an ordinary sibling is still admitted.
func TestAdmitLinksDropsStructuralTraps(t *testing.T) {
	wide, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:          yagocrawlcontract.ScopeWide,
		URLMustMatch:   yagocrawlcontract.MatchAll,
		AllowQueryURLs: true,
	})

	got := wide.AdmitLinks("https://example.com/", []string{
		"https://example.com/real/page",
		"https://example.com/cal/a/a/a/a/a",
	})
	want := []string{"https://example.com/real/page"}
	if !slices.Equal(got, want) {
		t.Errorf("admit dropping trap = %v want %v", got, want)
	}
}

// TestAdmitLinksDropsUnnormalizableURL pins that a link which clears scope and
// URL-pattern admission but has no host to canonicalize (an opaque http URL such
// as "http:opaque", which resolves with an empty host) is dropped at the
// normalization step and never reaches the frontier, while an ordinary sibling
// is still admitted.
func TestAdmitLinksDropsUnnormalizableURL(t *testing.T) {
	wide, _ := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeWide,
		URLMustMatch: yagocrawlcontract.MatchAll,
	})

	got := wide.AdmitLinks("https://example.com/", []string{
		"https://example.com/keep",
		"http:opaque",
	})
	want := []string{"https://example.com/keep"}
	if !slices.Equal(got, want) {
		t.Errorf("admit = %v, want %v (unnormalizable url dropped)", got, want)
	}
}

func TestAdmitLinksBadBaseURL(t *testing.T) {
	c, _ := crawladmission.CompileProfile(
		yagocrawlcontract.CrawlProfile{URLMustMatch: yagocrawlcontract.MatchAll},
	)
	if got := c.AdmitLinks("://bad", []string{"https://example.com/"}); got != nil {
		t.Errorf("bad base should admit nothing, got %v", got)
	}
}
