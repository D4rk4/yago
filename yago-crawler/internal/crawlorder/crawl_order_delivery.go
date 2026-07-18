package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type CrawlOrderDelivery struct {
	LeaseID        string
	Order          yagocrawlcontract.CrawlOrder
	OrderIdentity  []byte
	Ack            func(context.Context) error
	Nak            func(context.Context) error
	Term           func(context.Context) error
	settleTerminal func(context.Context, terminalRunSettlement) error
}

type terminalRunSettlement struct {
	Disposition    crawlOrderDisposition
	State          yagocrawlcontract.CrawlRunState
	Tally          yagocrawlcontract.CrawlRunTally
	PagesPerMinute uint32
	RateKnown      bool
}
