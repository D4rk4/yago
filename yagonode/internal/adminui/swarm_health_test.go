package adminui

import (
	"testing"
	"time"
)

// TestSwarmHealthScoreBands pins OPS-12: a peer seen minutes ago with a
// month's age and a day's uptime scores healthy; one silent for days decays
// toward stale; one never seen scores only its longevity and uptime shares.
func TestSwarmHealthScoreBands(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)

	fresh := SwarmHealthScore(now.Add(-10*time.Minute), true, 24*60, 45, now)
	if fresh < 95 {
		t.Fatalf("fresh mature peer = %d, want near 100", fresh)
	}
	if tag := SwarmHealthTag(fresh); tag != "healthy" {
		t.Fatalf("fresh tag = %q", tag)
	}

	silent := SwarmHealthScore(now.Add(-5*24*time.Hour), true, 0, 45, now)
	if silent >= 40 {
		t.Fatalf("five-days-silent peer = %d, want stale band", silent)
	}
	if tag := SwarmHealthTag(silent); tag != "stale" {
		t.Fatalf("silent tag = %q", tag)
	}

	never := SwarmHealthScore(time.Time{}, false, 12*60, 15, now)
	if never < 20 || never > 30 {
		t.Fatalf("never-seen peer = %d, want only longevity+uptime shares", never)
	}

	future := SwarmHealthScore(now.Add(time.Hour), true, 0, 0, now)
	if future != 0 {
		t.Fatalf("future last-seen should not earn recency: %d", future)
	}

	invalid := SwarmHealthScore(now, true, -100, -10, now)
	if invalid != 50 {
		t.Fatalf("invalid negative seed facts score = %d, want 50", invalid)
	}
}

func TestSwarmHealthTagBoundaries(t *testing.T) {
	if SwarmHealthTag(70) != "healthy" || SwarmHealthTag(69) != "aging" ||
		SwarmHealthTag(40) != "aging" || SwarmHealthTag(39) != "stale" {
		t.Fatal("band boundaries moved")
	}
}
