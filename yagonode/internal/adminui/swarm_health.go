package adminui

import (
	"math"
	"time"
)

// Swarm-health scoring from passive observation (OPS-12): every signal is
// already in the seed a peer gossips — no probing beyond the normal protocol
// traffic, and the resulting map is only ever shown to the local operator.
// The score mixes recency (how recently the swarm saw the peer; churn studies
// of long-lived P2P networks make last-contact the dominant availability
// predictor), longevity (peers that survived their first weeks tend to stay),
// and the peer's self-reported uptime.
const (
	swarmHealthRecencyPoints   = 50.0
	swarmHealthLongevityPoints = 25.0
	swarmHealthUptimePoints    = 25.0
	swarmHealthRecencyHalfLife = 24 * time.Hour
	swarmHealthMatureAgeDays   = 30
	swarmHealthFullUptime      = 24 * 60
)

// SwarmHealthScore rates one peer 0..100 from its gossiped seed facts.
// A peer never seen scores only its longevity and uptime shares.
func SwarmHealthScore(
	lastSeen time.Time,
	seen bool,
	uptimeMinutes int,
	ageDays int,
	now time.Time,
) int {
	score := 0.0
	if seen && !lastSeen.After(now) {
		age := now.Sub(lastSeen)
		score += swarmHealthRecencyPoints *
			math.Exp2(-age.Hours()/swarmHealthRecencyHalfLife.Hours())
	}
	score += swarmHealthLongevityPoints *
		math.Max(0, math.Min(float64(ageDays)/swarmHealthMatureAgeDays, 1))
	score += swarmHealthUptimePoints *
		math.Max(0, math.Min(float64(uptimeMinutes)/swarmHealthFullUptime, 1))

	return int(math.Round(math.Max(0, math.Min(score, 100))))
}

// SwarmHealthTag folds the score into the three operator-facing bands.
func SwarmHealthTag(score int) string {
	switch {
	case score >= 70:
		return "healthy"
	case score >= 40:
		return "aging"
	default:
		return "stale"
	}
}
