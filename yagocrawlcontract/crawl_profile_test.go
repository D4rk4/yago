package yagocrawlcontract_test

import (
	"fmt"
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

func TestPageBudgetChangesHandleWithoutChangingLegacyHandle(t *testing.T) {
	legacy := yagocrawlcontract.NewCrawlProfile(baseProfile())
	raw := fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s\x00%d\x00%s\x00%s",
		legacy.Name,
		legacy.URLMustMatch,
		legacy.MaxDepth,
		legacy.URLMustNotMatch,
		legacy.MaxPagesPerHost,
		legacy.IndexURLMustMatch,
		legacy.IndexURLMustNotMatch,
	)
	wantLegacy := yagomodel.YaCyHashBase64(raw)[:yagomodel.HashLength]
	if legacy.Handle != wantLegacy {
		t.Fatalf("legacy handle = %q, want %q", legacy.Handle, wantLegacy)
	}
	legacyAgain := yagocrawlcontract.NewCrawlProfile(baseProfile())
	if legacy.Handle != legacyAgain.Handle {
		t.Fatalf("legacy handles differ: %q and %q", legacy.Handle, legacyAgain.Handle)
	}

	limit := 500
	bounded := baseProfile()
	bounded.MaxPagesPerRun = &limit
	bounded = yagocrawlcontract.NewCrawlProfile(bounded)
	if bounded.Handle == legacy.Handle {
		t.Fatalf("bounded profile reused legacy handle %q", bounded.Handle)
	}

	otherLimit := 501
	other := baseProfile()
	other.MaxPagesPerRun = &otherLimit
	other = yagocrawlcontract.NewCrawlProfile(other)
	if other.Handle == bounded.Handle {
		t.Fatalf("different page budgets shared handle %q", bounded.Handle)
	}
}

func TestEffectiveMaxPagesPerRunPrefersExplicitLimitAndBoundsFallback(t *testing.T) {
	maximum := 321
	profile := yagocrawlcontract.CrawlProfile{MaxPagesPerRun: &maximum}
	if got := profile.EffectiveMaxPagesPerRun(654); got != maximum {
		t.Fatalf("explicit maximum = %d, want %d", got, maximum)
	}

	if got := (yagocrawlcontract.CrawlProfile{}).EffectiveMaxPagesPerRun(-1); got != 0 {
		t.Fatalf("negative fallback = %d, want zero", got)
	}
}

func TestValidCrawlRunLimitsDistinguishesWholeRunAndPerHostBounds(t *testing.T) {
	for _, limits := range [][2]int{{yagocrawlcontract.UnlimitedPagesPerHost, 0}, {250, 900}} {
		if !yagocrawlcontract.ValidCrawlRunLimits(limits[0], limits[1]) {
			t.Fatalf("valid limits rejected: %v", limits)
		}
	}
	for _, limits := range [][2]int{{0, 900}, {-2, 900}, {250, -1}} {
		if yagocrawlcontract.ValidCrawlRunLimits(limits[0], limits[1]) {
			t.Fatalf("invalid limits accepted: %v", limits)
		}
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
