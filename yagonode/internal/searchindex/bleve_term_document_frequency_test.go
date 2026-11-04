package searchindex

import (
	"context"
	"errors"
	"testing"
)

func TestBleveSearchDeadlineTermReaderReadsDocumentFrequencyOnce(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	probe := &bleveSearchDeadlineTermReaderProbe{count: 17}
	reader := newBleveSearchDeadlineTermReader(ctx, probe)
	for read := range 3 {
		if count := reader.Count(); count != 17 {
			t.Fatalf("read %d count=%d, want 17", read, count)
		}
	}
	if probe.countCalls != 1 {
		t.Fatalf("document frequency reads = %d, want 1", probe.countCalls)
	}
	cancel(errors.New("term read canceled"))
	if count := reader.Count(); count != 0 || probe.countCalls != 1 {
		t.Fatalf("canceled count/calls=%d/%d, want 0/1", count, probe.countCalls)
	}
}
