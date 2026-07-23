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
			if err := f.profiles.Put(tx, vault.Key(profile.Handle), profile); err != nil {
				return fmt.Errorf("write recrawl profile: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist missing recrawl profiles: %w", err)
	}

	return nil
}
