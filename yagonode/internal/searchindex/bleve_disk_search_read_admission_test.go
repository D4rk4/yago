package searchindex

import (
	"context"
	"errors"
	"testing"
	"time"
)

type acquiredCanceledSearchContext struct{}

func (acquiredCanceledSearchContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (acquiredCanceledSearchContext) Done() <-chan struct{} { return nil }

func (acquiredCanceledSearchContext) Err() error {
	return context.Canceled
}

func (acquiredCanceledSearchContext) Value(any) any { return nil }

func TestBleveDiskSearchReadParallelismTracksProcessors(t *testing.T) {
	for name, test := range map[string]struct {
		processors int
		want       int
	}{
		"four processors":    {processors: 4, want: 4},
		"sixteen processors": {processors: 16, want: 16},
		"invalid input":      {processors: 0, want: 1},
	} {
		t.Run(name, func(t *testing.T) {
			got := bleveDiskSearchReadParallelism(test.processors)
			if got != test.want {
				t.Fatalf("parallelism=%d, want=%d", got, test.want)
			}
		})
	}
}

func TestBleveDiskSearchReadAdmissionHonorsCancellationAndRelease(t *testing.T) {
	index := &BleveDiskIndex{searchReadAdmission: make(chan struct{}, 1)}
	release, err := index.admitSearchRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("search budget elapsed")
	cancel(cause)
	if _, err := index.admitSearchRead(canceled); !errors.Is(err, cause) {
		t.Fatalf("waiting admission error=%v, want=%v", err, cause)
	}
	release()

	finalRelease, err := index.admitSearchRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	finalRelease()
	if len(index.searchReadAdmission) != 0 {
		t.Fatalf("retained admissions=%d", len(index.searchReadAdmission))
	}
}

func TestBleveDiskSearchPageReturnsCanceledReadAdmission(t *testing.T) {
	admission := make(chan struct{}, 1)
	admission <- struct{}{}
	index := &BleveDiskIndex{searchReadAdmission: admission}
	ctx, cancel := context.WithCancelCause(t.Context())
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := index.readSearchPage(ctx, nil)
		result <- err
	}()
	<-started
	cause := errors.New("search read queue elapsed")
	cancel(cause)
	select {
	case err := <-result:
		if !errors.Is(err, cause) {
			t.Fatalf("search admission error=%v, want=%v", err, cause)
		}
	case <-time.After(time.Second):
		t.Fatal("search admission did not observe cancellation")
	}
	if len(admission) != 1 {
		t.Fatalf("search admission depth=%d, want=1", len(admission))
	}
}

func TestBleveDiskSearchReadAdmissionRejectsCancellationRace(t *testing.T) {
	index := &BleveDiskIndex{searchReadAdmission: make(chan struct{}, 1)}
	ctx := acquiredCanceledSearchContext{}
	if _, err := index.admitSearchRead(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation race error=%v", err)
	}
	if len(index.searchReadAdmission) != 0 {
		t.Fatalf("cancellation race retained admissions=%d", len(index.searchReadAdmission))
	}
}

func TestBleveDiskSearchReadAdmissionLeavesUnconfiguredIndexUnchanged(t *testing.T) {
	index := &BleveDiskIndex{}
	release, err := index.admitSearchRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
