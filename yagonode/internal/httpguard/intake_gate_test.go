package httpguard

import "testing"

func TestIntakeGateBoundsConcurrentAdmissions(t *testing.T) {
	gate := NewIntakeGate(2)

	releaseFirst, ok := gate.TryAcquire()
	if !ok {
		t.Fatal("first slot should be granted")
	}
	_, ok = gate.TryAcquire()
	if !ok {
		t.Fatal("second slot should be granted")
	}
	if _, ok := gate.TryAcquire(); ok {
		t.Fatal("third acquisition should be shed at limit 2")
	}

	releaseFirst()
	if _, ok := gate.TryAcquire(); !ok {
		t.Fatal("released slot should be grantable again")
	}
}

func TestIntakeGateNilAndNonPositiveAdmitEverything(t *testing.T) {
	if NewIntakeGate(0) != nil {
		t.Fatal("non-positive limit should build an unlimited nil gate")
	}
	var gate *IntakeGate
	for range 3 {
		release, ok := gate.TryAcquire()
		if !ok {
			t.Fatal("nil gate must admit everything")
		}
		release()
	}
}

func TestObservedIntakeGateCountsRejections(t *testing.T) {
	rejections := 0
	gate := NewObservedIntakeGate(1, func() { rejections++ })
	release, ok := gate.TryAcquire()
	if !ok {
		t.Fatal("first acquire must succeed")
	}
	if _, ok := gate.TryAcquire(); ok {
		t.Fatal("second acquire must shed")
	}
	if rejections != 1 {
		t.Fatalf("rejections = %d, want 1", rejections)
	}
	release()
	if _, ok := gate.TryAcquire(); !ok {
		t.Fatal("released slot must admit again")
	}
	if rejections != 1 {
		t.Fatalf("admitted request counted as rejection: %d", rejections)
	}

	if NewObservedIntakeGate(0, func() {}) != nil {
		t.Fatal("non-positive limit must return the nil gate")
	}
}
