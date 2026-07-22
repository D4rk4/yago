package peerroster

import "container/heap"

type rankedRosterEntries struct {
	entries  []rosterEntry
	precedes func(rosterEntry, rosterEntry) bool
}

func (r rankedRosterEntries) Len() int {
	return len(r.entries)
}

func (r rankedRosterEntries) Less(left, right int) bool {
	return r.precedes(r.entries[right], r.entries[left])
}

func (r rankedRosterEntries) Swap(left, right int) {
	r.entries[left], r.entries[right] = r.entries[right], r.entries[left]
}

func (r *rankedRosterEntries) Push(value any) {
	r.entries = append(r.entries, value.(rosterEntry))
}

func (r *rankedRosterEntries) Pop() any {
	last := len(r.entries) - 1
	entry := r.entries[last]
	r.entries[last] = rosterEntry{}
	r.entries = r.entries[:last]

	return entry
}

func (r *rankedRosterEntries) retain(entry rosterEntry, limit int) {
	if limit <= 0 {
		return
	}
	if len(r.entries) < limit {
		heap.Push(r, entry)

		return
	}
	if !r.precedes(entry, r.entries[0]) {
		return
	}
	heap.Pop(r)
	heap.Push(r, entry)
}
