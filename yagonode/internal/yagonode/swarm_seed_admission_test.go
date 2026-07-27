package yagonode

import "testing"

func TestSwarmSeedAdmissionRejectsWorkAtCapacity(t *testing.T) {
	admission := newSwarmSeedAdmission(1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !admission.try(func() {
		close(started)
		<-release
	}) {
		t.Fatal("first work item was rejected")
	}
	<-started
	if admission.try(func() {}) {
		t.Fatal("work above capacity was admitted")
	}
	close(release)
}

// Refusing at capacity is only half the gate: the slot has to come back when the
// work finishes. A gate that never returns its slots refuses identically to a
// saturated one, so greedy learning would seed the first swarmSeedConcurrentWrites
// queries of the process lifetime and then silently stop seeding forever, which
// no capacity test can distinguish from a busy node.
func TestSwarmSeedAdmissionReturnsTheSlotWhenWorkFinishes(t *testing.T) {
	admission := newSwarmSeedAdmission(1)
	var queued func()
	admission.launch = func(work func()) { queued = work }

	if !admission.try(func() {}) {
		t.Fatal("first work item was rejected")
	}
	if admission.try(func() {}) {
		t.Fatal("work above capacity was admitted")
	}
	queued()
	if !admission.try(func() {}) {
		t.Fatal("the finished work item did not return its slot")
	}
	queued()
}
