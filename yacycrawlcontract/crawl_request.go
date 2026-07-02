package yacycrawlcontract

import (
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type CrawlRequest struct {
	URL           string
	Mode          CrawlRequestMode
	ReferrerURL   string
	AnchorName    string
	Depth         int
	ProfileHandle string
	Initiator     yacymodel.Hash
	AppDate       time.Time
	LastModified  time.Time
}

type CrawlRequestMode string

const (
	CrawlRequestModeURL      CrawlRequestMode = "url"
	CrawlRequestModeSitemap  CrawlRequestMode = "sitemap"
	CrawlRequestModeSitelist CrawlRequestMode = "sitelist"
)

func NormalizeCrawlRequestMode(mode CrawlRequestMode) (CrawlRequestMode, bool) {
	switch mode {
	case "", CrawlRequestModeURL:
		return CrawlRequestModeURL, true
	case CrawlRequestModeSitemap, CrawlRequestModeSitelist:
		return mode, true
	default:
		return "", false
	}
}
