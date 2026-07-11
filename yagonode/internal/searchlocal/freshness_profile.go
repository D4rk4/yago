package searchlocal

import (
	"math"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type freshnessDecay struct {
	halfLife time.Duration
	weight   float64
}

type freshnessDecayProfile [3]freshnessDecay

func freshnessProfileFor(
	req searchcore.Request,
	results []searchcore.Result,
	now time.Time,
) freshnessDecayProfile {
	switch {
	case req.SortByDate:
		return decayProfile(
			[3]time.Duration{7 * 24 * time.Hour, 60 * 24 * time.Hour, 365 * 24 * time.Hour},
			[3]float64{0.7, 0.2, 0.1},
		)
	case !req.MinDate.IsZero() || !req.MaxDate.IsZero():
		return rangeFreshnessProfile(req, now)
	default:
		return adaptiveFreshnessProfile(results, now)
	}
}

func rangeFreshnessProfile(req searchcore.Request, now time.Time) freshnessDecayProfile {
	start := req.MinDate
	end := req.MaxDate
	if start.IsZero() {
		start = now.Add(-365 * 24 * time.Hour)
	}
	if end.IsZero() || end.After(now) {
		end = now
	}
	window := end.Sub(start)
	if window < 2*24*time.Hour {
		window = 2 * 24 * time.Hour
	}
	short := window / 2

	return decayProfile(
		[3]time.Duration{short, 4 * short, 16 * short},
		[3]float64{0.6, 0.3, 0.1},
	)
}

func adaptiveFreshnessProfile(
	results []searchcore.Result,
	now time.Time,
) freshnessDecayProfile {
	confident := 0.0
	recentMonth := 0.0
	recentHalfYear := 0.0
	for _, result := range results {
		published, err := time.Parse("20060102", result.Date)
		if err != nil || result.DateConfidence <= 0 {
			continue
		}
		confidence := min(1, result.DateConfidence)
		age := now.Sub(published)
		if age < 0 {
			age = 0
		}
		confident += confidence
		if age <= 30*24*time.Hour {
			recentMonth += confidence
		}
		if age <= 180*24*time.Hour {
			recentHalfYear += confidence
		}
	}
	if confident == 0 {
		return decayProfile(
			[3]time.Duration{30 * 24 * time.Hour, 180 * 24 * time.Hour, 730 * 24 * time.Hour},
			[3]float64{0.15, 0.35, 0.5},
		)
	}

	return decayProfile(
		[3]time.Duration{30 * 24 * time.Hour, 180 * 24 * time.Hour, 730 * 24 * time.Hour},
		[3]float64{
			0.15 + 0.5*recentMonth/confident,
			0.25 + 0.35*recentHalfYear/confident,
			0.5,
		},
	)
}

func decayProfile(
	halfLives [3]time.Duration,
	weights [3]float64,
) freshnessDecayProfile {
	total := weights[0] + weights[1] + weights[2]

	return freshnessDecayProfile{
		{halfLife: halfLives[0], weight: weights[0] / total},
		{halfLife: halfLives[1], weight: weights[1] / total},
		{halfLife: halfLives[2], weight: weights[2] / total},
	}
}

func (p freshnessDecayProfile) Score(age time.Duration) float64 {
	if age < 0 {
		age = 0
	}
	score := 0.0
	for _, decay := range p {
		if decay.halfLife > 0 && decay.weight > 0 {
			score += decay.weight * math.Exp2(-age.Hours()/decay.halfLife.Hours())
		}
	}

	return min(1, max(0, score))
}
