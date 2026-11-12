package yagocrawlcontract

import "time"

type SearchDemandRecord struct {
	Topic       string
	URL         string
	Shown       uint64
	Clicked     uint64
	LastSeen    time.Time
	LastClicked time.Time
	FocusedAt   time.Time
}

type SearchDemandPage struct {
	Records    []SearchDemandRecord
	NextCursor string
}
