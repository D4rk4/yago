package frontier

import (
	"context"
	"time"
)

func (frontier *Frontier) ClaimExcludingHosts(
	ctx context.Context,
	runID string,
	now time.Time,
	excludedHosts map[string]struct{},
) (Page, bool, error) {
	return frontier.claim(ctx, runID, now, excludedHosts)
}
