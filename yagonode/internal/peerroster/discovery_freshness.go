package peerroster

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func discoveredRosterEntry(seed yagomodel.Seed, now time.Time) (rosterEntry, bool) {
	entry := rosterEntry{seed: seed.Copy(), expiresAt: now.Add(peerPassiveRetention)}
	seen, known := seed.LastSeen.Get()
	if !known {
		entry.lastSeen = now
		return entry, true
	}
	entry.lastSeen = seen.Time()
	if entry.lastSeen.After(now) || now.Sub(entry.lastSeen) > peerPassiveRetention {
		return rosterEntry{}, false
	}
	entry.expiresAt = entry.lastSeen.Add(peerPassiveRetention)

	return entry, true
}

func verifiedRosterEntry(seed yagomodel.Seed, now time.Time) rosterEntry {
	return rosterEntry{
		seed:      seed.Copy(),
		lastSeen:  now,
		expiresAt: now.Add(peerPassiveRetention),
		verified:  true,
	}
}

func routingClassificationEligible(seed yagomodel.Seed) bool {
	classification, known := seed.PeerType.Get()
	if !known {
		return true
	}

	return classification == yagomodel.PeerSenior || classification == yagomodel.PeerPrincipal
}
