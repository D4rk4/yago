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
	// IgnoreTLSAuthority fetches https pages without verifying the certificate
	// chain, for self-signed or mis-chained sites an operator still wants
	// crawled. The crawl payload is public web content, not credentials.
	IgnoreTLSAuthority bool
	MaxPagesPerHost    int
	RecrawlIfOlder     time.Duration
	CrawlDelay         time.Duration
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
