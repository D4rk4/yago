package documentimport

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type Record struct {
	ID          string                           `json:"id"`
	Document    yagocrawlcontract.DocumentIngest `json:"document"`
	NextVisitAt *time.Time                       `json:"nextVisitAt,omitempty"`
}

type Result struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type Request struct {
	Version int      `json:"version"`
	DryRun  bool     `json:"dryRun,omitempty"`
	Records []Record `json:"records"`
}

type Importer interface {
	ImportDocuments(ctx context.Context, records []Record) ([]Result, error)
}

type ConcurrentSubmitter interface {
	MaximumConcurrentImports() int
}
