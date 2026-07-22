package peerroster

import (
	"container/heap"

	"github.com/D4rk4/yago/yagomodel"
)

type candidateSnapshotBounds struct {
	peerLimit int
	byteLimit int
}

type candidateSnapshotRetention struct {
	candidates    *freshestRosterCandidates
	retainedBytes int
	active        map[yagomodel.Hash]struct{}
	owners        map[string]endpointOwnership
	bounds        candidateSnapshotBounds
}

func (r *candidateSnapshotRetention) retain(entry rosterEntry) {
	if _, found := r.active[entry.seed.Hash]; found {
		return
	}
	if _, addressable := entry.seed.NetworkAddress(); !addressable ||
		!routingClassificationEligible(entry.seed) ||
		!entryOwnsAdvertisedEndpoints(r.owners, entry) {
		return
	}

	entryBytes := entry.seed.RetainedBytes() + rosterCandidateRetentionBytes
	if entryBytes > r.bounds.byteLimit {
		return
	}
	heap.Push(r.candidates, rosterCandidate{entry: entry, retainedBytes: entryBytes})
	r.retainedBytes += entryBytes
	for len(*r.candidates) > r.bounds.peerLimit || r.retainedBytes > r.bounds.byteLimit {
		removed := heap.Pop(r.candidates).(rosterCandidate)
		r.retainedBytes -= removed.retainedBytes
	}
}
