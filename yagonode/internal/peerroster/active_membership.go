package peerroster

import "github.com/D4rk4/yago/yagomodel"

func (r *roster) replaceActiveMembership(
	entry rosterEntry,
	displaced []yagomodel.Hash,
) {
	now := entry.lastSeen
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, r.self)
	for peer, current := range r.active {
		if !current.expiresAt.IsZero() && !now.Before(current.expiresAt) {
			delete(r.active, peer)
		}
	}
	for _, peer := range displaced {
		delete(r.active, peer)
	}
	if !routingClassificationEligible(entry.seed) {
		delete(r.active, entry.seed.Hash)
	} else if _, active := r.active[entry.seed.Hash]; active || len(r.active) < r.activeCap {
		r.active[entry.seed.Hash] = entry
	}
}
