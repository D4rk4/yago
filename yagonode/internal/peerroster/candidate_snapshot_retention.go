package peerroster

import (
	"container/heap"

	"github.com/D4rk4/yago/yagomodel"
)

type candidateSnapshotBounds struct {
	peerLimit int
	byteLimit int
}

func retainFreshestRosterCandidate(
	candidates *freshestRosterCandidates,
	retainedBytes int,
	entry rosterEntry,
	active map[yagomodel.Hash]struct{},
	bounds candidateSnapshotBounds,
) int {
	if _, found := active[entry.seed.Hash]; found {
		return retainedBytes
	}
	if _, addressable := entry.seed.NetworkAddress(); !addressable || locallyJunior(entry.seed) {
		return retainedBytes
	}

	entryBytes := entry.seed.RetainedBytes() + rosterCandidateRetentionBytes
	if entryBytes > bounds.byteLimit {
		return retainedBytes
	}
	heap.Push(candidates, rosterCandidate{entry: entry, retainedBytes: entryBytes})
	retainedBytes += entryBytes
	for len(*candidates) > bounds.peerLimit || retainedBytes > bounds.byteLimit {
		removed := heap.Pop(candidates).(rosterCandidate)
		retainedBytes -= removed.retainedBytes
	}

	return retainedBytes
}
