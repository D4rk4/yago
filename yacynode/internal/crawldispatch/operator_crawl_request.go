package crawldispatch

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
)

type operatorCrawlRequest struct {
	Name                string   `json:"name"`
	Seeds               []string `json:"seeds"`
	StartMode           string   `json:"startMode"`
	Scope               string   `json:"scope"`
	URLMustMatch        string   `json:"urlMustMatch"`
	URLMustNotMatch     string   `json:"urlMustNotMatch"`
	MaxDepth            int      `json:"maxDepth"`
	AllowQueryURLs      bool     `json:"allowQueryURLs"`
	FollowNoFollowLinks bool     `json:"followNoFollowLinks"`
	MaxPagesPerHost     int      `json:"maxPagesPerHost"`
	RecrawlIfOlder      string   `json:"recrawlIfOlder"`
	CrawlDelay          string   `json:"crawlDelay"`
}

var crawlScopeByName = map[string]yacycrawlcontract.CrawlScope{
	"":        yacycrawlcontract.ScopeDomain,
	"domain":  yacycrawlcontract.ScopeDomain,
	"wide":    yacycrawlcontract.ScopeWide,
	"subpath": yacycrawlcontract.ScopeSubpath,
}

func (r operatorCrawlRequest) order(
	initiator yacymodel.Hash,
	provenance []byte,
	now time.Time,
) (yacycrawlcontract.CrawlOrder, error) {
	if len(r.Seeds) == 0 {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("at least one seed url is required")
	}

	mode, ok := crawlRequestModeByName[r.StartMode]
	if !ok {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("unknown crawl startMode %q", r.StartMode)
	}

	scope, ok := crawlScopeByName[r.Scope]
	if !ok {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("unknown crawl scope %q", r.Scope)
	}

	recrawl, err := optionalDuration(r.RecrawlIfOlder)
	if err != nil {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("recrawlIfOlder: %w", err)
	}
	delay, err := optionalDuration(r.CrawlDelay)
	if err != nil {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("crawlDelay: %w", err)
	}

	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:                r.Name,
		Scope:               scope,
		URLMustMatch:        matchOrAll(r.URLMustMatch),
		URLMustNotMatch:     r.URLMustNotMatch,
		MaxDepth:            r.MaxDepth,
		AllowQueryURLs:      r.AllowQueryURLs,
		FollowNoFollowLinks: r.FollowNoFollowLinks,
		MaxPagesPerHost:     r.MaxPagesPerHost,
		RecrawlIfOlder:      recrawl,
		CrawlDelay:          delay,
	})
	if err := profile.Validate(); err != nil {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("invalid crawl profile: %w", err)
	}

	requests := make([]yacycrawlcontract.CrawlRequest, 0, len(r.Seeds))
	for _, seed := range r.Seeds {
		requests = append(requests, yacycrawlcontract.CrawlRequest{
			URL:           seed,
			Mode:          mode,
			ProfileHandle: profile.Handle,
			Initiator:     initiator,
			AppDate:       now,
		})
	}

	return yacycrawlcontract.CrawlOrder{
		Provenance: provenance,
		Profile:    profile,
		Requests:   requests,
	}, nil
}

var crawlRequestModeByName = map[string]yacycrawlcontract.CrawlRequestMode{
	"":         yacycrawlcontract.CrawlRequestModeURL,
	"url":      yacycrawlcontract.CrawlRequestModeURL,
	"sitemap":  yacycrawlcontract.CrawlRequestModeSitemap,
	"sitelist": yacycrawlcontract.CrawlRequestModeSitelist,
}

func matchOrAll(pattern string) string {
	if pattern == "" {
		return yacycrawlcontract.MatchAll
	}
	return pattern
}

func optionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	return value, nil
}
