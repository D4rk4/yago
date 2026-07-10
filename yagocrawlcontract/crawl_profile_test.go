package yagocrawlcontract_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func baseProfile() yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.CrawlProfile{
		Name:            "news",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        3,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	}
}

func TestHandleIsTwelveChars(t *testing.T) {
	handle := yagocrawlcontract.NewCrawlProfile(baseProfile()).Handle
	if len(handle) != yagomodel.HashLength {
		t.Errorf("handle length = %d, want %d", len(handle), yagomodel.HashLength)
	}
}

func TestIdenticalRulesetsShareHandle(t *testing.T) {
	a := yagocrawlcontract.NewCrawlProfile(baseProfile())
	b := yagocrawlcontract.NewCrawlProfile(baseProfile())
	if a.Handle != b.Handle {
		t.Errorf("identical rulesets gave %q and %q", a.Handle, b.Handle)
	}
}

func TestDifferingRulesetsDifferHandle(t *testing.T) {
	a := yagocrawlcontract.NewCrawlProfile(baseProfile())

	differing := baseProfile()
	differing.MaxDepth = 5
	b := yagocrawlcontract.NewCrawlProfile(differing)

	if a.Handle == b.Handle {
		t.Errorf("differing rulesets shared handle %q", a.Handle)
	}
}

func TestNonRulesetFieldsDoNotChangeHandle(t *testing.T) {
	a := yagocrawlcontract.NewCrawlProfile(baseProfile())

	cosmetic := baseProfile()
	cosmetic.AllowQueryURLs = true
	cosmetic.FollowNoFollowLinks = true
	cosmetic.NoindexCanonicalMismatch = true
	cosmetic.RecrawlIfOlder = 1
	cosmetic.CrawlDelay = 1
	b := yagocrawlcontract.NewCrawlProfile(cosmetic)

	if a.Handle != b.Handle {
		t.Errorf("ruleset-irrelevant fields changed handle: %q vs %q", a.Handle, b.Handle)
	}
}

// TestDefaultFormatTogglesEnablesEveryFamilyExceptArchives pins the shared
// crawl default: every parseable family is on out of the box while archives,
// whose unpacking is a deliberate security decision, stays off.
func TestDefaultFormatTogglesEnablesEveryFamilyExceptArchives(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	if !toggles.Text || !toggles.XMLFeeds || !toggles.PDF || !toggles.Office ||
		!toggles.Images || !toggles.Audio || !toggles.Misc {
		t.Fatalf("default toggles must enable every parseable family: %+v", toggles)
	}
	if toggles.Archives {
		t.Fatal("archives must default off")
	}
}
