package yagocrawlcontract

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type CrawlScope int

const (
	ScopeWide CrawlScope = iota
	ScopeDomain
	ScopeSubpath
)

const MatchAll = ".*"

const UnlimitedPagesPerHost = -1

type CrawlProfile struct {
	Handle               string
	Name                 string
	Scope                CrawlScope
	URLMustMatch         string
	URLMustNotMatch      string
	IndexURLMustMatch    string
	IndexURLMustNotMatch string
	MaxDepth             int
	AllowQueryURLs       bool
	FollowNoFollowLinks  bool
	// NoindexCanonicalMismatch crawls a page whose parsed rel=canonical
	// resolves to a different URL than the fetched page's normalized URL for
	// links only, without indexing it, mirroring YaCy's
	// NOINDEX_WHEN_CANONICAL_UNEQUAL_URL. Default off: canonical often points
	// paginated pages at page 1, which would silently drop them.
	NoindexCanonicalMismatch bool
	// IgnoreTLSAuthority fetches https pages without verifying the certificate
	// chain, for self-signed or mis-chained sites an operator still wants
	// crawled. The crawl payload is public web content, not credentials.
	IgnoreTLSAuthority bool
	// IgnoreRobots fetches pages without consulting robots.txt. Robots is
	// enforced by default; this is an explicit per-profile operator opt-out the
	// admin form confirms, for archiving one's own sites or hosts whose robots
	// file blocks all crawlers by mistake.
	IgnoreRobots bool
	// DisableBrowser keeps this crawl on the fast HTTP path only: the headless
	// browser never escalates a rejected fetch. Off by default — bot-walled
	// pages keep rendering — but a browser tab is heavy, so profiles crawling
	// plain-HTML corpora can opt out.
	DisableBrowser  bool
	MaxPagesPerHost int
	RecrawlIfOlder  time.Duration
	CrawlDelay      time.Duration
	// Formats selects which document format families the crawler parses and
	// indexes for this crawl; the node fills it from the operator's shared
	// format settings on every dispatch.
	Formats FormatToggles
}

// FormatToggles enables document format families for parsing (YaCy TextParser
// parity). HTML/web text is always on and carries no toggle.
type FormatToggles struct {
	// Text: txt, tex, csv, rtf, msg.
	Text bool
	// XMLFeeds: xml, rss, atom.
	XMLFeeds bool
	// PDF: pdf, ps.
	PDF bool
	// Office: OOXML, OpenDocument/StarOffice, legacy Office, Visio, FreeMind.
	Office bool
	// Images: jpg, png, gif, bmp, wbmp, tiff, psd, svg metadata.
	Images bool
	// Audio: mp3, ogg, wma, wav, m4a/m4b/m4p, mp4, aiff, ra/rm, sid tags.
	Audio bool
	// Misc: vcf, torrent, apk.
	Misc bool
	// Archives: zip, jar, epub, tar, gz/tgz, bz2/tbz/tbz2, xz/txz containers.
	// Default off: unpacking hostile archives is a security decision.
	Archives bool
}

// DefaultFormatToggles enables every family except archives.
func DefaultFormatToggles() FormatToggles {
	return FormatToggles{
		Text:     true,
		XMLFeeds: true,
		PDF:      true,
		Office:   true,
		Images:   true,
		Audio:    true,
		Misc:     true,
	}
}

func NewCrawlProfile(profile CrawlProfile) CrawlProfile {
	profile.Handle = profile.ComputeHandle()
	return profile
}

func (p CrawlProfile) ComputeHandle() string {
	raw := fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s\x00%d\x00%s\x00%s",
		p.Name, p.URLMustMatch, p.MaxDepth, p.URLMustNotMatch, p.MaxPagesPerHost,
		p.IndexURLMustMatch, p.IndexURLMustNotMatch,
	)
	return yagomodel.YaCyHashBase64(raw)[:yagomodel.HashLength]
}
