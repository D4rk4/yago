package yagonode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	crawlBrokerStateFileName                   = "crawlbroker.db"
	crawlRuntimeStateOpenTimeout               = 5 * time.Second
	crawlRuntimeStateProvisioningHeadroomBytes = 1 << 20
)

var (
	openCrawlRuntimeStateVault = func(path string) (*vault.Vault, error) {
		return boltvault.OpenWithLockTimeout(path, crawlRuntimeStateOpenTimeout)
	}
	migrateCrawlBrokerState     = crawlbroker.MigrateLegacyStorageWithAdmission
	migrateCrawlRunState        = crawlruns.MigrateLegacyStorageWithAdmission
	closeCrawlRuntimeStateVault = func(state *vault.Vault) error {
		return state.Close()
	}
)

func openCrawlRuntimeState(
	ctx context.Context,
	path string,
	legacy *vault.Vault,
	admissions ...growthAdmission,
) (*vault.Vault, bool, error) {
	if strings.TrimSpace(path) == "" {
		return legacy, false, nil
	}
	if legacy == nil {
		return nil, false, fmt.Errorf("open crawl runtime state: legacy storage unavailable")
	}
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}
	state, err := openCrawlRuntimeStateStorage(path, admission)
	if err != nil {
		return nil, false, fmt.Errorf("open crawl runtime state: %w", err)
	}
	if err := migrateCrawlBrokerState(ctx, legacy, state, admission); err != nil {
		return nil, false, crawlRuntimeStateFailure(
			fmt.Errorf("migrate crawl broker state: %w", err),
			state,
			true,
		)
	}
	if err := migrateCrawlRunState(ctx, legacy, state, admission); err != nil {
		return nil, false, crawlRuntimeStateFailure(
			fmt.Errorf("migrate crawl run state: %w", err),
			state,
			true,
		)
	}
	return state, true, nil
}

func openCrawlRuntimeStateStorage(
	path string,
	admission growthAdmission,
) (*vault.Vault, error) {
	requiresProvisioning, err := crawlRuntimeStateRequiresProvisioning(path)
	if err != nil {
		return nil, err
	}
	if !requiresProvisioning {
		return openCrawlRuntimeStateVault(path)
	}
	var state *vault.Vault
	_, err = runStorageMaintenance(
		admission,
		func() (uint64, error) {
			return crawlRuntimeStateProvisioningHeadroomBytes, nil
		},
		func(uint64) error {
			var openErr error
			state, openErr = openCrawlRuntimeStateVault(path)

			return openErr
		},
	)
	if err != nil {
		return nil, fmt.Errorf("provision crawl runtime state: %w", err)
	}

	return state, nil
}

func crawlRuntimeStateRequiresProvisioning(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect crawl runtime state: %w", err)
	}

	return info.Size() == 0, nil
}

func closeOwnedCrawlRuntimeState(state *vault.Vault, owned bool) error {
	if owned && state != nil {
		return closeCrawlRuntimeStateVault(state)
	}

	return nil
}

func crawlRuntimeStateFailure(failure error, state *vault.Vault, owned bool) error {
	if closeErr := closeOwnedCrawlRuntimeState(state, owned); closeErr != nil {
		return errors.Join(failure, fmt.Errorf("close crawl runtime state: %w", closeErr))
	}

	return failure
}
