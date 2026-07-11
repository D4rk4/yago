package searchlocal

import (
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestFreshnessProfileUsesStructuredDateIntent(t *testing.T) {
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	dateSort := freshnessProfileFor(searchcore.Request{SortByDate: true}, nil, now)
	if dateSort[0].halfLife != 7*24*time.Hour || math.Abs(dateSort[0].weight-0.7) > 1e-12 {
		t.Fatalf("date-sort profile = %#v", dateSort)
	}
	rangeProfile := freshnessProfileFor(searchcore.Request{
		MinDate: now.Add(-20 * 24 * time.Hour), MaxDate: now.Add(24 * time.Hour),
	}, nil, now)
	if rangeProfile[0].halfLife != 10*24*time.Hour ||
		rangeProfile[1].halfLife != 40*24*time.Hour {
		t.Fatalf("range profile = %#v", rangeProfile)
	}
	shortRange := freshnessProfileFor(searchcore.Request{
		MinDate: now.Add(-time.Hour), MaxDate: now,
	}, nil, now)
	if shortRange[0].halfLife != 24*time.Hour {
		t.Fatalf("short range profile = %#v", shortRange)
	}
	openStart := freshnessProfileFor(searchcore.Request{MaxDate: now}, nil, now)
	if openStart[0].halfLife <= 0 {
		t.Fatalf("open-start profile = %#v", openStart)
	}
	openEnd := freshnessProfileFor(
		searchcore.Request{MinDate: now.Add(-8 * 24 * time.Hour)},
		nil,
		now,
	)
	if openEnd[0].halfLife != 4*24*time.Hour {
		t.Fatalf("open-end profile = %#v", openEnd)
	}
}

func TestFreshnessProfileAdaptsToCandidateDates(t *testing.T) {
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	neutral := freshnessProfileFor(searchcore.Request{}, []searchcore.Result{{
		Date: "bad", DateConfidence: 1,
	}}, now)
	if neutral[0].weight != 0.15 || neutral[2].weight != 0.5 {
		t.Fatalf("neutral profile = %#v", neutral)
	}
	recent := freshnessProfileFor(searchcore.Request{}, []searchcore.Result{
		{Date: now.Add(-5 * 24 * time.Hour).Format("20060102"), DateConfidence: 2},
		{Date: now.Add(-90 * 24 * time.Hour).Format("20060102"), DateConfidence: 0.5},
		{Date: now.Add(24 * time.Hour).Format("20060102"), DateConfidence: 1},
		{Date: now.Add(-900 * 24 * time.Hour).Format("20060102")},
	}, now)
	if recent[0].weight <= neutral[0].weight || recent[1].weight <= 0 ||
		math.Abs(recent[0].weight+recent[1].weight+recent[2].weight-1) > 1e-12 {
		t.Fatalf("recent profile = %#v", recent)
	}
}

func TestFreshnessDecayProfileScoresAndBounds(t *testing.T) {
	profile := decayProfile(
		[3]time.Duration{24 * time.Hour, 2 * 24 * time.Hour, 4 * 24 * time.Hour},
		[3]float64{1, 1, 2},
	)
	if profile.Score(-time.Hour) != 1 || profile.Score(365*24*time.Hour) <= 0 ||
		profile.Score(365*24*time.Hour) >= 0.01 {
		t.Fatalf(
			"profile scores = %v/%v",
			profile.Score(-time.Hour),
			profile.Score(365*24*time.Hour),
		)
	}
	empty := freshnessDecayProfile{{halfLife: 0, weight: 1}, {halfLife: time.Hour, weight: 0}}
	if empty.Score(time.Hour) != 0 {
		t.Fatalf("empty profile score = %v", empty.Score(time.Hour))
	}
}
