package crawljob

import (
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type CrawlJob struct {
	URL           string
	Depth         int
	ProfileHandle string
	Provenance    []byte
	RunID         uuid.UUID
	Index         bool
	// CrawlDelay is the profile's requested politeness delay for this job's host.
	// Zero means the crawler's global default delay applies.
	CrawlDelay time.Duration
	// Formats selects which document format families this job may parse.
	Formats yagocrawlcontract.FormatToggles
	// IgnoreTLSAuthority routes this job through the fetch chain that skips
	// certificate-chain verification (profile opt-in).
	IgnoreTLSAuthority bool
	// IgnoreRobots routes this job through the fetch chain that skips the
	// robots.txt check (explicit profile opt-out; obeyed by default).
	IgnoreRobots bool
	// DisableBrowser keeps this job on the fast HTTP path: no headless-browser
	// escalation when the fast fetch is rejected.
	DisableBrowser bool
}

type DiscoveredLinks struct {
	Followable []string
	NoFollow   []string
}

func (l DiscoveredLinks) ByPolicy(followNoFollow bool) []string {
	if followNoFollow {
		links := make([]string, 0, len(l.Followable)+len(l.NoFollow))
		links = append(links, l.Followable...)
		links = append(links, l.NoFollow...)
		return links
	}
	return append([]string(nil), l.Followable...)
}
