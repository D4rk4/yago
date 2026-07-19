package yagonode

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type crawlRuntimeStateStartupLeaseProbe struct {
	held       *bool
	releaseErr error
}

func (lease crawlRuntimeStateStartupLeaseProbe) Release() error {
	*lease.held = false

	return lease.releaseErr
}

func TestCrawlRuntimeStateStartupLeaseCoversAuthoritativeOpen(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	held := false
	acquireCrawlRuntimeStateStartupLease = func(
		string,
		time.Duration,
	) (crawlRuntimeStateStartupLease, error) {
		held = true

		return crawlRuntimeStateStartupLeaseProbe{held: &held}, nil
	}
	opened := openTestVault(t)
	openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) {
		if !held {
			t.Fatal("authoritative crawl state opened after startup lease release")
		}

		return opened, nil
	}
	state, err := openCrawlRuntimeStateStorage(
		filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		nil,
	)
	if err != nil || state != opened || held {
		t.Fatalf("leased crawl state open = %p, held=%t, error=%v", state, held, err)
	}
	if err := state.Close(); err != nil {
		t.Fatalf("close leased crawl state: %v", err)
	}
}

func TestCrawlRuntimeStateStartupLeaseFailuresAreFailStop(t *testing.T) {
	t.Run("acquire", func(t *testing.T) {
		restoreCrawlRuntimeStateSeams(t)
		want := errors.New("lease acquisition failed")
		acquireCrawlRuntimeStateStartupLease = func(
			string,
			time.Duration,
		) (crawlRuntimeStateStartupLease, error) {
			return nil, want
		}
		opened := false
		openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) {
			opened = true

			return nil, nil
		}
		state, err := openCrawlRuntimeStateStorage(
			filepath.Join(t.TempDir(), crawlBrokerStateFileName),
			nil,
		)
		if state != nil || opened || !errors.Is(err, want) {
			t.Fatalf("acquisition failure = %p, opened=%t, error=%v", state, opened, err)
		}
	})

	t.Run("release after open", func(t *testing.T) {
		restoreCrawlRuntimeStateSeams(t)
		want := errors.New("lease release failed")
		held := false
		acquireCrawlRuntimeStateStartupLease = func(
			string,
			time.Duration,
		) (crawlRuntimeStateStartupLease, error) {
			held = true

			return crawlRuntimeStateStartupLeaseProbe{held: &held, releaseErr: want}, nil
		}
		opened := openTestVault(t)
		openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) { return opened, nil }
		closed := false
		closeCrawlRuntimeStateVault = func(state *vault.Vault) error {
			closed = true

			return state.Close()
		}
		state, err := openCrawlRuntimeStateStorage(
			filepath.Join(t.TempDir(), crawlBrokerStateFileName),
			nil,
		)
		if state != nil || !closed || held || !errors.Is(err, want) {
			t.Fatalf("release failure = %p, closed=%t, held=%t, error=%v", state, closed, held, err)
		}
	})

	t.Run("release after open failure", func(t *testing.T) {
		restoreCrawlRuntimeStateSeams(t)
		wantOpen := errors.New("state open failed")
		wantRelease := errors.New("lease release failed")
		held := false
		acquireCrawlRuntimeStateStartupLease = func(
			string,
			time.Duration,
		) (crawlRuntimeStateStartupLease, error) {
			held = true

			return crawlRuntimeStateStartupLeaseProbe{
				held: &held, releaseErr: wantRelease,
			}, nil
		}
		openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) {
			return nil, wantOpen
		}
		state, err := openCrawlRuntimeStateStorage(
			filepath.Join(t.TempDir(), crawlBrokerStateFileName),
			nil,
		)
		if state != nil || held || !errors.Is(err, wantOpen) || !errors.Is(err, wantRelease) {
			t.Fatalf("combined startup failure = %p, held=%t, error=%v", state, held, err)
		}
	})
}
