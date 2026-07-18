package frontiercheckpoint

import (
	"fmt"
	"math"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func addRunTally(
	current yagocrawlcontract.CrawlRunTally,
	delta yagocrawlcontract.CrawlRunTally,
) (yagocrawlcontract.CrawlRunTally, error) {
	values := []*uint64{
		&current.Fetched,
		&current.Indexed,
		&current.Failed,
		&current.RobotsDenied,
		&current.Duplicates,
	}
	deltas := []uint64{
		delta.Fetched,
		delta.Indexed,
		delta.Failed,
		delta.RobotsDenied,
		delta.Duplicates,
	}
	for index, value := range values {
		if math.MaxUint64-*value < deltas[index] {
			return yagocrawlcontract.CrawlRunTally{}, fmt.Errorf(
				"%w: run tally overflow",
				ErrCorruptCheckpoint,
			)
		}
		*value += deltas[index]
	}
	current.Pending = 0

	return current, nil
}
