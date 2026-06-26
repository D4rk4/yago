package crawldispatch

import (
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type operatorCrawlRequest struct {
	Name            string   `json:"name"`
	Seeds           []string `json:"seeds"`
	Scope           string   `json:"scope"`
	URLMustMatch    string   `json:"urlMustMatch"`
	URLMustNotMatch string   `json:"urlMustNotMatch"`
	MaxDepth        int      `json:"maxDepth"`
	AllowQueryURLs  bool     `json:"allowQueryURLs"`
	MaxPagesPerHost int      `json:"maxPagesPerHost"`
	RecrawlIfOlder  string   `json:"recrawlIfOlder"`
	CrawlDelay      string   `json:"crawlDelay"`
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

	scope, ok := crawlScopeByName[r.Scope]
	if !ok {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf("unknown crawl scope %q", r.Scope)
	}

	if r.MaxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost && r.MaxPagesPerHost <= 0 {
		return yacycrawlcontract.CrawlOrder{}, fmt.Errorf(
			"maxPagesPerHost must be positive or %d for unlimited",
			yacycrawlcontract.UnlimitedPagesPerHost,
		)
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
		Name:            r.Name,
		Scope:           scope,
		URLMustMatch:    matchOrAll(r.URLMustMatch),
		URLMustNotMatch: r.URLMustNotMatch,
		MaxDepth:        r.MaxDepth,
		AllowQueryURLs:  r.AllowQueryURLs,
		MaxPagesPerHost: r.MaxPagesPerHost,
		RecrawlIfOlder:  recrawl,
		CrawlDelay:      delay,
	})

	requests := make([]yacycrawlcontract.CrawlRequest, 0, len(r.Seeds))
	for _, seed := range r.Seeds {
		requests = append(requests, yacycrawlcontract.CrawlRequest{
			URL:           seed,
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
