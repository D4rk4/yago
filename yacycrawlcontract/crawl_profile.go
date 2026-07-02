package yacycrawlcontract

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
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
	Handle              string
	Name                string
	Scope               CrawlScope
	URLMustMatch        string
	URLMustNotMatch     string
	MaxDepth            int
	AllowQueryURLs      bool
	FollowNoFollowLinks bool
	MaxPagesPerHost     int
	RecrawlIfOlder      time.Duration
	CrawlDelay          time.Duration
}

func NewCrawlProfile(profile CrawlProfile) CrawlProfile {
	profile.Handle = profile.ComputeHandle()
	return profile
}

func (p CrawlProfile) ComputeHandle() string {
	raw := fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s\x00%d",
		p.Name, p.URLMustMatch, p.MaxDepth, p.URLMustNotMatch, p.MaxPagesPerHost,
	)
	return yacymodel.YaCyHashBase64(raw)[:yacymodel.HashLength]
}
