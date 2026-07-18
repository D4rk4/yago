package frontier

import (
	"fmt"
	"math"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func platformPageTotal(value uint64) (int, error) {
	if value > uint64(math.MaxInt) {
		return 0, fmt.Errorf(
			"%w: page total exceeds platform capacity",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}

	return int(value), nil
}

func recoveryPageTotal(value uint64) (int, error) {
	if value > uint64(frontiercheckpoint.RecoveryPageBatchSize) {
		return 0, fmt.Errorf(
			"%w: recovery page total exceeds batch capacity",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}

	return int(value), nil
}

func seedCursorAdvance(value int) (uint64, error) {
	if value < 0 || value > frontierMutationBatchSize {
		return 0, fmt.Errorf(
			"%w: seed cursor advance exceeds batch capacity",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}

	return uint64(value), nil
}
