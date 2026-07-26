package recrawlfrontier

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (f *Frontier) persistMissingProfiles(
	ctx context.Context,
	profiles []yagocrawlcontract.CrawlProfile,
) error {
	unique := make([]yagocrawlcontract.CrawlProfile, 0, len(profiles))
	positions := make(map[string]int, len(profiles))
	for _, profile := range profiles {
		if profile.Handle == "" {
			return fmt.Errorf("empty profile handle")
		}
		if position, found := positions[profile.Handle]; found {
			unique[position] = profile
			continue
		}
		positions[profile.Handle] = len(unique)
		unique = append(unique, profile)
	}
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, profile := range unique {
			_, found, err := f.profiles.Get(tx, vault.Key(profile.Handle))
			if err != nil {
				return fmt.Errorf("read recrawl profile: %w", err)
			}
			if found {
				continue
			}
			// Ingest carries the profile from the live lease but not the order
			// priority, so a backfilled record leaves it unset. This only ever
			// fills a gap left by a failed or evicted dispatch registration —
			// an existing record, and the priority it carries, is never
			// rewritten here — and the next dispatch under the handle restores
			// the authoritative priority.
			record := profileRecord{CrawlProfile: profile}
			if err := f.profiles.Put(tx, vault.Key(profile.Handle), record); err != nil {
				return fmt.Errorf("write recrawl profile: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist missing recrawl profiles: %w", err)
	}

	return nil
}
