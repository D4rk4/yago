package events_test

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

func record(r *events.Recorder, name string) {
	r.Record(events.SeverityInfo, events.CategoryConfig, name, "message "+name)
}

func names(evs []events.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Name
	}

	return out
}

func TestRecorderReturnsNewestFirst(t *testing.T) {
	r := events.NewRecorder(8)
	record(r, "a")
	record(r, "b")
	record(r, "c")

	if got := names(r.Recent(0)); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Fatalf("recent = %v, want [c b a]", got)
	}
}

func TestRecorderEmptyReturnsNone(t *testing.T) {
	if got := events.NewRecorder(4).Recent(0); len(got) != 0 {
		t.Fatalf("recent = %v, want none", got)
	}
}

func TestRecorderWrapsAndDropsOldest(t *testing.T) {
	r := events.NewRecorder(3)
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		record(r, n)
	}

	if got := names(r.Recent(0)); !reflect.DeepEqual(got, []string{"5", "4", "3"}) {
		t.Fatalf("recent = %v, want [5 4 3]", got)
	}
}

func TestRecorderRespectsLimit(t *testing.T) {
	r := events.NewRecorder(8)
	for _, n := range []string{"1", "2", "3", "4"} {
		record(r, n)
	}

	if got := names(r.Recent(2)); !reflect.DeepEqual(got, []string{"4", "3"}) {
		t.Fatalf("recent = %v, want [4 3]", got)
	}
}

func TestRecorderLimitAboveCountReturnsAll(t *testing.T) {
	r := events.NewRecorder(8)
	record(r, "a")
	record(r, "b")

	if got := names(r.Recent(100)); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("recent = %v, want [b a]", got)
	}
}

func TestRecorderZeroCapacityUsesDefault(t *testing.T) {
	r := events.NewRecorder(0)
	record(r, "x")

	got := r.Recent(0)
	if len(got) != 1 || got[0].Name != "x" ||
		got[0].Severity != events.SeverityInfo || got[0].Category != events.CategoryConfig {
		t.Fatalf("recent = %#v", got)
	}
}
