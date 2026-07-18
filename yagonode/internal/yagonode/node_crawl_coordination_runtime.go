package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type crawlCoordinationRuntime struct {
	broker    *crawlbroker.CrawlBroker
	runs      *crawlruns.Registry
	state     *vault.Vault
	ownsState bool
}

func openCrawlCoordinationRuntime(
	ctx context.Context,
	config crawlConfig,
	legacy *vault.Vault,
) (crawlCoordinationRuntime, error) {
	state, ownsState, err := openCrawlRuntimeState(
		ctx,
		config.StatePath,
		legacy,
		config.GrowthAdmission,
	)
	if err != nil {
		return crawlCoordinationRuntime{}, err
	}
	runs := crawlruns.New(0)
	if state != nil {
		runs, err = crawlruns.Open(ctx, state, 0)
		if err != nil {
			return crawlCoordinationRuntime{}, crawlRuntimeStateFailure(
				fmt.Errorf("open crawl run registry: %w", err),
				state,
				ownsState,
			)
		}
	}
	broker, err := openCrawlBroker(
		crawlbroker.Config{
			ListenAddr:                        config.ListenAddr,
			FetchWorkers:                      config.FetchWorkers,
			DisableAutomaticDiscoveryPriority: !config.PrioritizeAutomaticDiscovery,
			StoragePressurePolicy:             crawlerStoragePressurePolicy(config),
			GrowthAdmission:                   config.GrowthAdmission,
		},
		state,
		runs,
	)
	if err != nil {
		return crawlCoordinationRuntime{}, crawlRuntimeStateFailure(
			fmt.Errorf("open crawl broker: %w", err),
			state,
			ownsState,
		)
	}

	return crawlCoordinationRuntime{
		broker: broker, runs: runs, state: state, ownsState: ownsState,
	}, nil
}

func (runtime crawlCoordinationRuntime) openFailure(failure error) error {
	runtime.broker.Close()

	return crawlRuntimeStateFailure(failure, runtime.state, runtime.ownsState)
}
