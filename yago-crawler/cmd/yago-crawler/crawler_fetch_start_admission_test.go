package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type crawlerFetchStartAdmissionCall struct {
	name  string
	calls *[]string
	err   error
}

func (admission crawlerFetchStartAdmissionCall) Wait(context.Context) error {
	*admission.calls = append(*admission.calls, admission.name)

	return admission.err
}

func TestOrderedCrawlerFetchStartAdmissionRunsProcessBeforeFleet(t *testing.T) {
	calls := make([]string, 0, 2)
	admission := newOrderedCrawlerFetchStartAdmission(
		crawlerFetchStartAdmissionCall{name: "process", calls: &calls},
		crawlerFetchStartAdmissionCall{name: "fleet", calls: &calls},
	)
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("ordered admission: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"process", "fleet"}) {
		t.Fatalf("admission order = %v", calls)
	}
}

func TestOrderedCrawlerFetchStartAdmissionStopsAtFailure(t *testing.T) {
	processFailure := errors.New("process admission failed")
	calls := make([]string, 0, 2)
	admission := newOrderedCrawlerFetchStartAdmission(
		crawlerFetchStartAdmissionCall{name: "process", calls: &calls, err: processFailure},
		crawlerFetchStartAdmissionCall{name: "fleet", calls: &calls},
	)
	if err := admission.Wait(t.Context()); !errors.Is(err, processFailure) {
		t.Fatalf("process failure = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"process"}) {
		t.Fatalf("failed admission order = %v", calls)
	}

	fleetFailure := errors.New("fleet admission failed")
	calls = calls[:0]
	admission = newOrderedCrawlerFetchStartAdmission(
		crawlerFetchStartAdmissionCall{name: "process", calls: &calls},
		crawlerFetchStartAdmissionCall{name: "fleet", calls: &calls, err: fleetFailure},
	)
	if err := admission.Wait(t.Context()); !errors.Is(err, fleetFailure) {
		t.Fatalf("fleet failure = %v", err)
	}
}
