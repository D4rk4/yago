package dhtexchange

import (
	"slices"

	"github.com/D4rk4/yago/yagomodel"
)

func (q *OutboundQueue) retainPendingRestore(postings []yagomodel.RWIPosting) {
	q.pendingRestores = slices.Clone(postings)
}

func (q *OutboundQueue) pendingRestore() []yagomodel.RWIPosting {
	return slices.Clone(q.pendingRestores)
}

func (q *OutboundQueue) completePendingRestore() {
	q.pendingRestores = nil
}

func (q *OutboundQueue) retainPendingTransferConfirmation(
	postings []yagomodel.RWIPosting,
) {
	q.pendingTransferConfirmations = slices.Clone(postings)
}

func (q *OutboundQueue) pendingTransferConfirmation() []yagomodel.RWIPosting {
	return slices.Clone(q.pendingTransferConfirmations)
}

func (q *OutboundQueue) completePendingTransferConfirmation() {
	q.pendingTransferConfirmations = nil
}

func (q *OutboundQueue) localCompletionPending() bool {
	return len(q.pendingRestores) != 0 || len(q.pendingTransferConfirmations) != 0
}
