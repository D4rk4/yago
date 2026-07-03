// Package events records recent structured node events in a bounded in-memory
// ring so operators and the admin UI can review important activity without
// scraping logs.
package events

import "time"

type Severity string

const (
	SeverityDebug Severity = "debug"
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Category string

const (
	CategoryP2P      Category = "p2p"
	CategoryDHT      Category = "dht"
	CategorySearch   Category = "search"
	CategoryCrawl    Category = "crawl"
	CategoryStorage  Category = "storage"
	CategorySecurity Category = "security"
	CategoryConfig   Category = "config"
)

type Event struct {
	Time     time.Time
	Severity Severity
	Category Category
	Name     string
	Message  string
}
