package yagonode

import (
	"context"
	"log/slog"
)

const msgWebSeedWiring = "web-search crawl seeding wiring"

// reportWebSeedWiring states, once per assembly, whether web-discovery seeding
// can run at all and why not when it cannot. Until this existed an operator had
// no way to tell "seeding is switched off" from "seeding is broken": the seed
// path only ever logs on failure or saturation, so a silenced seeder and a
// healthy idle one looked identical.
func reportWebSeedWiring(config webFallbackConfig, queued bool) {
	reason := ""
	switch {
	case !queued:
		reason = "no crawl order queue on this node"
	case !config.SeedCrawl:
		reason = "web.fallback.seed_crawl is disabled (live-appliable in the console)"
	}
	slog.InfoContext(
		context.Background(),
		msgWebSeedWiring,
		slog.Bool("enabled", queued && config.SeedCrawl),
		slog.String("reason", reason),
		slog.Int("seedDepth", config.SeedDepth),
		slog.Int("seedMaxPages", config.SeedMaxPages),
	)
}

// gatedWebCrawlSeeder wraps the web-fallback crawl seeder in the live
// "web.fallback.seed_crawl" toggle. The seeder is wired once at assembly
// whenever a crawl queue exists, so an operator flipping web-discovery
// crawling in the console takes effect on the next search instead of waiting
// for a node restart.
//
// The gate closes at AdmitCrawlSeedURL, not just at Seed: resultURLs collects
// admitted URLs before any work is spawned, so admitting them while seeding is
// off would burn the bounded seed-admission slots and log spurious saturation
// warnings for work that is then discarded.
type gatedWebCrawlSeeder struct {
	seeder  *webCrawlSeeder
	enabled func() bool
}

// webSeedCrawlAdmission resolves the live seeding switch. Assemblies that carry
// runtime toggles follow the console; assemblies without them (the search
// explanation surface and tests) keep the boot configuration's value.
func webSeedCrawlAdmission(
	toggles *runtimeToggles,
	config webFallbackConfig,
) func() bool {
	if toggles == nil {
		return func() bool { return config.SeedCrawl }
	}

	return toggles.WebSeedCrawlEnabled
}

// webSeedBoundsSource resolves the per-order crawl bounds the same way, so a
// console edit to the depth or page cap reaches the very next seeded order.
func webSeedBoundsSource(
	toggles *runtimeToggles,
	config webFallbackConfig,
) func() seedBounds {
	fixed := seedBounds{depth: config.SeedDepth, maxPages: config.SeedMaxPages}
	if toggles == nil {
		return func() seedBounds { return fixed }
	}

	return func() seedBounds {
		return seedBounds{
			depth:    int(toggles.webSeedDepth.Load()),
			maxPages: int(toggles.webSeedMaxPages.Load()),
		}
	}
}

func newGatedWebCrawlSeeder(
	seeder *webCrawlSeeder,
	enabled func() bool,
) *gatedWebCrawlSeeder {
	return &gatedWebCrawlSeeder{seeder: seeder, enabled: enabled}
}

func (s *gatedWebCrawlSeeder) AdmitCrawlSeedURL(raw string) (string, bool) {
	if !s.admitted() {
		return "", false
	}

	return s.seeder.AdmitCrawlSeedURL(raw)
}

func (s *gatedWebCrawlSeeder) Seed(ctx context.Context, urls []string) {
	if !s.admitted() {
		return
	}
	s.seeder.Seed(ctx, urls)
}

func (s *gatedWebCrawlSeeder) admitted() bool {
	return s.seeder != nil && (s.enabled == nil || s.enabled())
}

// swarmSeedCrawlAdmission and swarmSeedBoundsSource are the greedy-learning
// counterparts of the web-discovery sources above. Greedy learning seeds one
// task per remote result, so it carries the same per-query multiplier and needs
// the same live throttle; assemblies without runtime toggles keep the boot
// configuration.
func swarmSeedCrawlAdmission(
	toggles *runtimeToggles,
	config swarmSeedConfig,
) func() bool {
	if toggles == nil {
		return func() bool { return config.Enabled }
	}

	return toggles.SwarmSeedCrawlEnabled
}

func swarmSeedBoundsSource(
	toggles *runtimeToggles,
	config swarmSeedConfig,
) func() seedBounds {
	fixed := seedBounds{depth: config.SeedDepth, maxPages: config.SeedMaxPages}
	if toggles == nil {
		return func() seedBounds { return fixed }
	}

	return func() seedBounds {
		return seedBounds{
			depth:    int(toggles.swarmSeedDepth.Load()),
			maxPages: int(toggles.swarmSeedMaxPages.Load()),
		}
	}
}
