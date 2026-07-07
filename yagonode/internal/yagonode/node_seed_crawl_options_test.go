package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// assertProfileHonorsOptions checks the five per-crawl toggles reached the
// built crawl profile intact.
func assertProfileHonorsOptions(
	t *testing.T,
	profile yagocrawlcontract.CrawlProfile,
	options seedCrawlOptions,
) {
	t.Helper()

	if profile.AllowQueryURLs != options.AllowQueryURLs ||
		profile.IgnoreTLSAuthority != options.IgnoreTLSAuthority ||
		profile.IgnoreRobots != options.IgnoreRobots ||
		profile.DisableBrowser != options.DisableBrowser ||
		profile.FollowNoFollowLinks != options.FollowNoFollowLinks {
		t.Fatalf("profile options = %+v, want %+v", profile, options)
	}
}

// TestNewCrawlSeederAppliesSeedCrawlOptions proves the autocrawler's per-crawl
// toggles reach the published crawl profile through both seeder constructors.
func TestNewCrawlSeederAppliesSeedCrawlOptions(t *testing.T) {
	options := seedCrawlOptions{
		AllowQueryURLs:      true,
		IgnoreTLSAuthority:  true,
		IgnoreRobots:        true,
		DisableBrowser:      true,
		FollowNoFollowLinks: true,
	}

	direct := newCrawlSeeder(
		nullCrawlQueue{},
		countingDirectory{},
		yagomodel.Hash("node"),
		seedProfile{name: swarmSeedProfileName, depth: 1, maxPages: 5, options: options},
	)
	assertProfileHonorsOptions(t, direct.profile, options)

	web := newWebCrawlSeeder(
		nullCrawlQueue{},
		countingDirectory{},
		yagomodel.Hash("node"),
		webFallbackConfig{SeedDepth: 1, SeedMaxPages: 5},
		options,
	)
	assertProfileHonorsOptions(t, web.profile, options)
}

// TestDefaultSeedCrawlOptions pins the shipped autocrawler crawl policy: query
// URLs and lax TLS on, the rest off.
func TestDefaultSeedCrawlOptions(t *testing.T) {
	got := defaultSeedCrawlOptions()
	want := seedCrawlOptions{AllowQueryURLs: true, IgnoreTLSAuthority: true}
	if got != want {
		t.Fatalf("defaultSeedCrawlOptions() = %+v, want %+v", got, want)
	}
}
