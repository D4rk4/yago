package yagocrawlcontract

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type CrawlRequest struct {
	URL            string
	Mode           CrawlRequestMode
	ReferrerURL    string
	AnchorName     string
	Depth          int
	ProfileHandle  string
	Initiator      yagomodel.Hash
	AppDate        time.Time
	LastModified   time.Time
	SourceMetadata *CrawlSourceMetadata `json:",omitempty"`
}

func ParseCrawlInitiator(raw []byte) (yagomodel.Hash, error) {
	initiator, err := yagomodel.ParseHash(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse crawl initiator: %w", err)
	}
	return initiator, nil
}

type CrawlRequestMode string

const (
	CrawlRequestModeURL      CrawlRequestMode = "url"
	CrawlRequestModeSitemap  CrawlRequestMode = "sitemap"
	CrawlRequestModeSitelist CrawlRequestMode = "sitelist"
	CrawlRequestModeRobots   CrawlRequestMode = "robots"
)

func NormalizeCrawlRequestMode(mode CrawlRequestMode) (CrawlRequestMode, bool) {
	switch mode {
	case "", CrawlRequestModeURL:
		return CrawlRequestModeURL, true
	case CrawlRequestModeSitemap, CrawlRequestModeSitelist, CrawlRequestModeRobots:
		return mode, true
	default:
		return "", false
	}
}
