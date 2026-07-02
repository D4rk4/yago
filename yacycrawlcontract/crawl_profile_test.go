package yacycrawlcontract_test

import (
	"testing"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
)

func baseProfile() yacycrawlcontract.CrawlProfile {
	return yacycrawlcontract.CrawlProfile{
		Name:            "news",
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        3,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	}
}

func TestHandleIsTwelveChars(t *testing.T) {
	handle := yacycrawlcontract.NewCrawlProfile(baseProfile()).Handle
	if len(handle) != yacymodel.HashLength {
		t.Errorf("handle length = %d, want %d", len(handle), yacymodel.HashLength)
	}
}

func TestIdenticalRulesetsShareHandle(t *testing.T) {
	a := yacycrawlcontract.NewCrawlProfile(baseProfile())
	b := yacycrawlcontract.NewCrawlProfile(baseProfile())
	if a.Handle != b.Handle {
		t.Errorf("identical rulesets gave %q and %q", a.Handle, b.Handle)
	}
}

func TestDifferingRulesetsDifferHandle(t *testing.T) {
	a := yacycrawlcontract.NewCrawlProfile(baseProfile())

	differing := baseProfile()
	differing.MaxDepth = 5
	b := yacycrawlcontract.NewCrawlProfile(differing)

	if a.Handle == b.Handle {
		t.Errorf("differing rulesets shared handle %q", a.Handle)
	}
}

func TestNonRulesetFieldsDoNotChangeHandle(t *testing.T) {
	a := yacycrawlcontract.NewCrawlProfile(baseProfile())

	cosmetic := baseProfile()
	cosmetic.AllowQueryURLs = true
	cosmetic.RecrawlIfOlder = 1
	cosmetic.CrawlDelay = 1
	b := yacycrawlcontract.NewCrawlProfile(cosmetic)

	if a.Handle != b.Handle {
		t.Errorf("ruleset-irrelevant fields changed handle: %q vs %q", a.Handle, b.Handle)
	}
}
