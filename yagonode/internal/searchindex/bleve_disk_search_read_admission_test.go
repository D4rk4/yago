package searchindex

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type acquiredCanceledSearchContext struct{}

type observedSearchContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (ctx *observedSearchContext) Err() error {
	ctx.once.Do(func() { close(ctx.checked) })
	switch ctx.Context.Err() {
	case context.Canceled:
		return context.Canceled
	case context.DeadlineExceeded:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (acquiredCanceledSearchContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (acquiredCanceledSearchContext) Done() <-chan struct{} { return nil }

func (acquiredCanceledSearchContext) Err() error {
	return context.Canceled
}

func (acquiredCanceledSearchContext) Value(any) any { return nil }

func TestBleveDiskSearchReadParallelismFollowsShardCapacity(t *testing.T) {
	for name, test := range map[string]struct {
		processors int
		shards     int
		want       int
	}{
		"bounded minimum": {processors: 4, shards: 8, want: 1},
		"one shard":       {processors: 4, shards: 1, want: 4},
		"two searches":    {processors: 16, shards: 8, want: 2},
		"invalid inputs":  {processors: 0, shards: 0, want: 1},
	} {
		t.Run(name, func(t *testing.T) {
			got := bleveDiskSearchReadParallelism(test.processors, test.shards)
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

func TestBleveDiskSearchReturnsCanceledReadAdmission(t *testing.T) {
	admission := make(chan struct{}, 1)
	admission <- struct{}{}
	index := &BleveDiskIndex{searchReadAdmission: admission}
	base, cancel := context.WithCancelCause(t.Context())
	ctx := &observedSearchContext{Context: base, checked: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, err := index.Search(
			ctx,
			SearchRequest{Query: "needle", MaxResults: 1},
		)
		result <- err
	}()
	<-ctx.checked
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
