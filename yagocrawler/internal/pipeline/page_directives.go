package pipeline

import (
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

// pageDirectives is a page's effective robots outcome after combining its
// meta robots tag, its X-Robots-Tag response header, and the profile's
// canonical-mismatch opt-in: noindex keeps the page out of the index while
// its links are still discovered, nofollow suppresses link discovery. The
// sources name what asked for each effect, for the debug log.
type pageDirectives struct {
	noindex        bool
	nofollow       bool
	noindexSource  string
	nofollowSource string
}

// effectiveDirectives combines a page's robots signals with the job's profile
// flags (CRAWL-28/CRAWL-29): IgnoreRobots waives both page-level directives,
// FollowNoFollowLinks waives page-level nofollow, and the profile's
// canonical-mismatch opt-in adds a noindex when rel=canonical points at a
// different URL. Noindex and nofollow combine independently.
func effectiveDirectives(
	job crawljob.CrawlJob,
	page pageparse.ParsedPage,
	robotsTag string,
) pageDirectives {
	var directives pageDirectives
	headerNoindex, headerNofollow := pageparse.RobotsDirectives(robotsTag)
	if !job.IgnoreRobots {
		directives.noindex = page.MetaNoindex || headerNoindex
		directives.noindexSource = directiveSource(page.MetaNoindex, headerNoindex)
		directives.nofollow = (page.MetaNofollow || headerNofollow) &&
			!job.FollowNoFollowLinks
		directives.nofollowSource = directiveSource(page.MetaNofollow, headerNofollow)
	}
	if !directives.noindex && job.NoindexCanonicalMismatch && canonicalDiffers(page) {
		directives.noindex = true
		directives.noindexSource = "canonical"
	}

	return directives
}

func directiveSource(meta, header bool) string {
	switch {
	case meta && header:
		return "meta+header"
	case meta:
		return "meta"
	case header:
		return "header"
	}

	return ""
}

// canonicalDiffers reports whether the page's parsed rel=canonical (already
// normalized by the parser) resolves to a different URL than the fetched
// page's own URL under the same normalization.
func canonicalDiffers(page pageparse.ParsedPage) bool {
	if page.CanonicalURL == "" {
		return false
	}
	norm, ok := weburl.Normalize(page.URL)
	if !ok {
		return false
	}

	return page.CanonicalURL != norm
}
