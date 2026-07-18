package frontier

import (
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func (run *crawlRun) hostOutcomeGeneration(host string) uint64 {
	return run.hostGenerations[host]
}

func (run *crawlRun) advanceHostOutcomeGeneration(host string) (uint64, error) {
	generation := run.hostGenerations[host]
	if generation == math.MaxUint64 {
		return 0, fmt.Errorf(
			"%w: host outcome generation overflow",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	generation++
	run.hostGenerations[host] = generation
	return generation, nil
}

func (f *Frontier) advanceHostOutcomeLocked(
	runID uuid.UUID,
	run *crawlRun,
	host string,
	durable bool,
) (uint64, bool) {
	generation, err := run.advanceHostOutcomeGeneration(host)
	if err == nil {
		return generation, true
	}
	if durable {
		f.finishRunDurabilityLocked(runID, run, err)
	}
	return 0, false
}
