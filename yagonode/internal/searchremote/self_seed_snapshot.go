package searchremote

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagomodel"
)

func (s searcher) withSelfSeedSnapshot(ctx context.Context) searcher {
	if s.selfSeed == nil {
		return s
	}
	resolve := s.selfSeed
	var once sync.Once
	var seed yagomodel.Seed
	s.selfSeed = func(context.Context) yagomodel.Seed {
		once.Do(func() {
			seed = resolve(ctx)
		})

		return seed
	}

	return s
}
