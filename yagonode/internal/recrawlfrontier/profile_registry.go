package recrawlfrontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// RecordProfile stores a crawl profile by its handle so that, when one of its
// pages later reports a fetch, the recrawl interval is known, and so the sweeper
// can rebuild a faithful crawl order for a due URL. Re-recording the same handle
// overwrites, keeping a profile's evolving fields current.
func (f *Frontier) RecordProfile(
	ctx context.Context,
	profile yagocrawlcontract.CrawlProfile,
) error {
	if profile.Handle == "" {
		return fmt.Errorf("record recrawl profile: empty handle")
	}
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := f.profiles.Put(tx, vault.Key(profile.Handle), profile); err != nil {
			return fmt.Errorf("write recrawl profile: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("record recrawl profile: %w", err)
	}

	return nil
}

// ProfileByHandle returns the crawl profile recorded for handle, if any. The
// sweeper uses it to turn a due URL back into a full crawl order.
func (f *Frontier) ProfileByHandle(
	ctx context.Context,
	handle string,
) (yagocrawlcontract.CrawlProfile, bool, error) {
	var (
		profile yagocrawlcontract.CrawlProfile
		found   bool
	)
	if err := f.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		profile, found, err = f.profiles.Get(tx, vault.Key(handle))
		if err != nil {
			return fmt.Errorf("read recrawl profile: %w", err)
		}

		return nil
	}); err != nil {
		return yagocrawlcontract.CrawlProfile{}, false, fmt.Errorf("profile by handle: %w", err)
	}

	return profile, found, nil
}

// RecordFetch schedules url for recrawl from the profile recorded for
// profileHandle: it becomes due at fetchedAt plus that profile's RecrawlIfOlder.
// If no profile is known for the handle, or the profile never recrawls, the fetch
// is not scheduled — recording profiles at dispatch is what makes handles known.
func (f *Frontier) RecordFetch(
	ctx context.Context,
	url, profileHandle string,
	fetchedAt time.Time,
) error {
	profile, found, err := f.ProfileByHandle(ctx, profileHandle)
	if err != nil {
		return fmt.Errorf("record fetch: %w", err)
	}
	if !found {
		return nil
	}
	if err := f.Observe(ctx, url, profileHandle, profile.RecrawlIfOlder, fetchedAt); err != nil {
		return fmt.Errorf("record fetch: %w", err)
	}

	return nil
}
