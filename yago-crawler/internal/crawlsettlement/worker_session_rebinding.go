package crawlsettlement

import (
	"context"
)

type WorkerSessionRebinder interface {
	RebindWorkerSession(
		context.Context,
		Settlement,
		string,
	) (Settlement, bool, error)
}

func SameDefinitionExceptWorkerSession(left Settlement, right Settlement) bool {
	left.WorkerSessionID = right.WorkerSessionID

	return SameDefinition(left, right)
}
