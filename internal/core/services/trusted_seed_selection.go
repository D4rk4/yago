package services

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type seedShuffler func(n int, swap func(i, j int))

func selectAnnouncedSeeds(
	seeds []yacymodel.Seed,
	count int,
	shuffle seedShuffler,
) []yacymodel.Seed {
	picked := make([]yacymodel.Seed, len(seeds))
	copy(picked, seeds)

	shuffle(len(picked), func(i, j int) {
		picked[i], picked[j] = picked[j], picked[i]
	})

	if count > 0 && count < len(picked) {
		picked = picked[:count]
	}

	return picked
}
