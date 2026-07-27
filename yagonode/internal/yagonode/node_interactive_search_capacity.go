package yagonode

import "runtime"

// interactiveSearchMinimumConcurrentWork floors the search pipeline's
// concurrency gate. The gate was a flat 4 while the HTTP gate in front of it
// admitted maximumConcurrentPublicSearches callers, so on a busy node the
// surplus queued behind it, spent the whole interactive budget waiting for a
// slot, and were answered with an empty result set and HTTP 200 -- a reply no
// client can tell apart from "nothing matched". Sizing the gate off the
// processor count lets a multi-core node serve what its front door admits.
const interactiveSearchMinimumConcurrentWork = 4

func interactiveSearchCapacity(procs int) int {
	if procs > interactiveSearchMinimumConcurrentWork {
		return procs
	}

	return interactiveSearchMinimumConcurrentWork
}

var interactiveSearchConcurrentWork = interactiveSearchCapacity(runtime.GOMAXPROCS(0))
