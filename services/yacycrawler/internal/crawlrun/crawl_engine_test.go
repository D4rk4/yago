package crawlrun_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlrun"
)

type fakeTraversal struct {
	err   error
	calls int
}

func (f *fakeTraversal) Traverse(context.Context, crawlcapability.DeliveredOrder) error {
	f.calls++
	return f.err
}

type recordingObserver struct {
	received    int
	completed   int
	redelivered int
}

func (o *recordingObserver) OrderReceived()    { o.received++ }
func (o *recordingObserver) OrderCompleted()   { o.completed++ }
func (o *recordingObserver) OrderRedelivered() { o.redelivered++ }

func (*recordingObserver) PageFetched()                {}
func (*recordingObserver) PublicationWaited()          {}
func (*recordingObserver) FetchObserved(time.Duration) {}
func (*recordingObserver) PageDisposed(string)         {}
func (*recordingObserver) PagePublished(string)        {}
func (*recordingObserver) RefusalHonored(string)       {}
func (*recordingObserver) BudgetExhausted()            {}

type settlement struct {
	acked   int
	retried int
	ackErr  error
}

func deliver(t *testing.T, engine *crawlrun.Engine, s *settlement) {
	t.Helper()
	deliveries := make(chan crawlcapability.DeliveredOrder, 1)
	deliveries <- crawlcapability.DeliveredOrder{
		Order: yacycrawlcontract.CrawlOrder{OrderID: "o1"},
		Ack:   func(context.Context) error { s.acked++; return s.ackErr },
		Retry: func(context.Context) error { s.retried++; return nil },
	}
	close(deliveries)
	if err := engine.Run(context.Background(), deliveries); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunAcksAndCompletesOnSuccess(t *testing.T) {
	observer := &recordingObserver{}
	engine := crawlrun.NewEngine(observer, &fakeTraversal{})
	s := &settlement{}

	deliver(t, engine, s)
	if s.acked != 1 || observer.completed != 1 || observer.received != 1 {
		t.Fatalf("expected ack+complete: %+v observer=%+v", s, observer)
	}
}

func TestRunRedeliversOnTraversalError(t *testing.T) {
	observer := &recordingObserver{}
	engine := crawlrun.NewEngine(observer, &fakeTraversal{err: errors.New("boom")})
	s := &settlement{}

	deliver(t, engine, s)
	if s.retried != 1 || s.acked != 0 || observer.redelivered != 1 || observer.completed != 0 {
		t.Fatalf("expected redelivery: %+v observer=%+v", s, observer)
	}
}

func TestRunToleratesAckError(t *testing.T) {
	observer := &recordingObserver{}
	engine := crawlrun.NewEngine(observer, &fakeTraversal{})
	s := &settlement{ackErr: errors.New("ack failed")}

	deliver(t, engine, s)
	if observer.completed != 0 {
		t.Fatalf("ack failure should not count as completed: %+v", observer)
	}
}

func TestRunStopsWhenDeliveriesClose(t *testing.T) {
	engine := crawlrun.NewEngine(&recordingObserver{}, &fakeTraversal{})
	deliveries := make(chan crawlcapability.DeliveredOrder)
	close(deliveries)

	if err := engine.Run(context.Background(), deliveries); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunStopsWhenContextCancelled(t *testing.T) {
	engine := crawlrun.NewEngine(&recordingObserver{}, &fakeTraversal{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := engine.Run(ctx, make(chan crawlcapability.DeliveredOrder)); err == nil {
		t.Fatal("cancelled context should end the run with an error")
	}
}
