package frontier

import (
	"fmt"
	"math"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func extendBoundedRecovery(run *crawlRun, admitted uint64) error {
	if !run.boundedRecovery || admitted == 0 {
		return nil
	}
	if admitted > math.MaxUint64-run.recoveryUpper {
		return fmt.Errorf(
			"%w: bounded recovery boundary overflow",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	run.recoveryUpper += admitted
	run.recoveryComplete = run.recoveryCursor == run.recoveryUpper

	return nil
}
