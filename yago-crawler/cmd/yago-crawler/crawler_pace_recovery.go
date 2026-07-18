package main

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

type hostPaceLedger interface {
	HostPaces(context.Context, int) (map[string]crawlpace.HostState, error)
}

func restoreCrawlerHostPaces(
	ctx context.Context,
	ledger hostPaceLedger,
	pace crawlpace.Checkpoint,
) error {
	states, err := ledger.HostPaces(context.WithoutCancel(ctx), pace.Capacity())
	if err != nil {
		return fmt.Errorf("load crawler host pace: %w", err)
	}
	for host, state := range states {
		pace.RestoreHost(host, state)
	}

	return nil
}
