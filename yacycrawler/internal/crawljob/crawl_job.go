package crawljob

import (
	"github.com/google/uuid"
)

type CrawlJob struct {
	URL           string
	Depth         int
	ProfileHandle string
	Provenance    []byte
	RunID         uuid.UUID
}
