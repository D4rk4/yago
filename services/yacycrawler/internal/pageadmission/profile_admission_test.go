package pageadmission_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageadmission"
)

func profile(p yacycrawlcontract.CrawlProfile) yacycrawlcontract.CrawlProfile {
	if p.URLMustMatch == "" {
		p.URLMustMatch = yacycrawlcontract.MatchAll
	}
	if p.MaxPagesPerHost == 0 {
		p.MaxPagesPerHost = yacycrawlcontract.UnlimitedPagesPerHost
	}
	return yacycrawlcontract.NewCrawlProfile(p)
}

func TestAdmitWideScope(t *testing.T) {
	admission, err := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeWide, MaxDepth: 2}),
		[]string{"http://a.com/"}, 100,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !admission.Admit("http://other.com/x", 1) {
		t.Fatal("wide scope should admit any host")
	}
}

func TestAdmitDomainScope(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeDomain, MaxDepth: 2}),
		[]string{"http://a.com/"}, 100,
	)
	if !admission.Admit("http://a.com/x", 1) {
		t.Fatal("same host should be admitted")
	}
	if admission.Admit("http://b.com/x", 1) {
		t.Fatal("other host should be rejected in domain scope")
	}
}

func TestAdmitSubpathScope(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeSubpath, MaxDepth: 3}),
		[]string{"http://a.com/dir/"}, 100,
	)
	if !admission.Admit("http://a.com/dir/page", 1) {
		t.Fatal("subpath child should be admitted")
	}
	if admission.Admit("http://a.com/other/page", 1) {
		t.Fatal("outside subpath should be rejected")
	}
}

func TestAdmitDepthLimit(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeWide, MaxDepth: 1}),
		[]string{"http://a.com/"}, 100,
	)
	if admission.Admit("http://a.com/deep", 2) {
		t.Fatal("beyond max depth should be rejected")
	}
}

func TestAdmitDuplicateRejected(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeWide, MaxDepth: 2}),
		[]string{"http://a.com/"}, 100,
	)
	if !admission.Admit("http://a.com/x", 1) {
		t.Fatal("first admit should succeed")
	}
	if admission.Admit("http://a.com/x", 1) {
		t.Fatal("duplicate should be rejected")
	}
}

func TestAdmitFrontierCap(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeWide, MaxDepth: 5}),
		[]string{"http://a.com/"}, 1,
	)
	if !admission.Admit("http://a.com/1", 1) {
		t.Fatal("first within cap")
	}
	if admission.Admit("http://a.com/2", 1) {
		t.Fatal("beyond frontier cap should be rejected")
	}
}

func TestAdmitPerHostCap(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{
			Scope: yacycrawlcontract.ScopeWide, MaxDepth: 5, MaxPagesPerHost: 1,
		}),
		[]string{"http://a.com/"}, 100,
	)
	if !admission.Admit("http://a.com/1", 1) {
		t.Fatal("first for host")
	}
	if admission.Admit("http://a.com/2", 1) {
		t.Fatal("beyond per-host cap should be rejected")
	}
}

func TestAdmitQueryRejectedWhenDisallowed(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{Scope: yacycrawlcontract.ScopeWide, MaxDepth: 2}),
		[]string{"http://a.com/"}, 100,
	)
	if admission.Admit("http://a.com/x?q=1", 1) {
		t.Fatal("query URL should be rejected by default")
	}
}

func TestAdmitMustMatchAndMustNotMatch(t *testing.T) {
	admission, _ := pageadmission.New(
		profile(yacycrawlcontract.CrawlProfile{
			Scope: yacycrawlcontract.ScopeWide, MaxDepth: 2,
			URLMustMatch: `\.html$`, URLMustNotMatch: `/private/`,
		}),
		[]string{"http://a.com/"}, 100,
	)
	if !admission.Admit("http://a.com/page.html", 1) {
		t.Fatal("matching URL should admit")
	}
	if admission.Admit("http://a.com/page.pdf", 1) {
		t.Fatal("non-matching should reject")
	}
	if admission.Admit("http://a.com/private/x.html", 1) {
		t.Fatal("must-not-match should reject")
	}
}

func TestNewRejectsBadRegex(t *testing.T) {
	if _, err := pageadmission.New(
		yacycrawlcontract.CrawlProfile{URLMustMatch: "("}, nil, 10,
	); err == nil {
		t.Fatal("bad must-match regex should error")
	}
	if _, err := pageadmission.New(
		yacycrawlcontract.CrawlProfile{URLMustMatch: ".*", URLMustNotMatch: "("}, nil, 10,
	); err == nil {
		t.Fatal("bad must-not-match regex should error")
	}
}
