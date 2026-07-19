package adminui

import (
	"context"
	"crypto/rand"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	DefaultExtractionRecrawlLimit = 20
	MaximumExtractionRecrawlLimit = 100
)

type ExtractionRecrawlSource interface {
	QueueOutdatedExtractions(
		ctx context.Context,
		actionID string,
		continuation string,
		limit int,
	) (ExtractionRecrawlResult, error)
}

type ExtractionRecrawlResult struct {
	Limit             int
	CurrentGeneration uint64
	Examined          int
	Visible           int
	CurrentOrNewer    int
	Outdated          int
	Queued            int
	AlreadyQueued     int
	ActionID          string
	Continuation      string
	Partial           bool
	Retry             bool
}

type ExtractionRecrawlView struct {
	Enabled           bool
	CurrentGeneration uint64
	Default           int
	Maximum           int
	ActionID          string
	Result            *ExtractionRecrawlResult
	Error             string
}

func newExtractionRecrawlView(
	enabled bool,
	result *ExtractionRecrawlResult,
	err string,
) ExtractionRecrawlView {
	view := ExtractionRecrawlView{
		Enabled:           enabled,
		CurrentGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		Default:           DefaultExtractionRecrawlLimit,
		Maximum:           MaximumExtractionRecrawlLimit,
		Result:            result,
		Error:             err,
	}
	if !enabled {
		return view
	}
	actionID := rand.Text()
	if result != nil && (result.Partial || result.Retry) &&
		validExtractionRecrawlActionID(result.ActionID) {
		actionID = result.ActionID
	}

	view.ActionID = actionID

	return view
}

func validExtractionRecrawlActionID(value string) bool {
	if len(value) != 26 {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') &&
			(character < '2' || character > '7') {
			return false
		}
	}

	return true
}
