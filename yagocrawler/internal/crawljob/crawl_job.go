package crawljob

import (
	"time"

	"github.com/google/uuid"
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
