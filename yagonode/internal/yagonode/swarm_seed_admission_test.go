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
