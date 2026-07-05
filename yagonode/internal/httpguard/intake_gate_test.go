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
