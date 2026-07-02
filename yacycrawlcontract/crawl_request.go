package yacycrawlcontract

import (
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type CrawlRequest struct {
	URL           string
	ReferrerURL   string
	AnchorName    string
	Depth         int
	ProfileHandle string
	Initiator     yacymodel.Hash
	AppDate       time.Time
}
