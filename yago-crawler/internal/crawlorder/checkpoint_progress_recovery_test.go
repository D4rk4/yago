package crawlorder_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type recoveryEventLog struct {
	mu      sync.Mutex
	events  []string
	reports []crawlorder.RunReport
}

func (log *recoveryEventLog) append(event string) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.events = append(log.events, event)
}

func (log *recoveryEventLog) ReportRun(_ context.Context, report crawlorder.RunReport) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.events = append(log.events, "report")
	log.reports = append(log.reports, report)
}

func (log *recoveryEventLog) snapshot() ([]string, []crawlorder.RunReport) {
	log.mu.Lock()
	defer log.mu.Unlock()

	return append([]string(nil), log.events...), append([]crawlorder.RunReport(nil), log.reports...)
}

type terminalRecoveryCheckpoint struct {
	frontier.Checkpoint
	state   frontiercheckpoint.RunState
	events  *recoveryEventLog
	deleted chan struct{}
}

type terminalRecoveryCase struct {
	name            string
	state           frontiercheckpoint.RunState
	wantState       yagocrawlcontract.CrawlRunState
	wantTally       yagocrawlcontract.CrawlRunTally
	wantDisposition string
	attachTally     bool
}

func (checkpoint terminalRecoveryCheckpoint) Inspect(
	context.Context,
	[]byte,
	[]byte,
) (frontiercheckpoint.RunState, error) {
	return checkpoint.state, nil
}

func (checkpoint terminalRecoveryCheckpoint) Delete(context.Context, []byte) error {
	checkpoint.events.append("delete")
	close(checkpoint.deleted)

	return nil
}

func TestTerminalRecoveryReportsPersistedTallyBeforeSettlementAndDeletion(t *testing.T) {
	cases := []terminalRecoveryCase{
		{
			name: "completed",
			state: frontiercheckpoint.RunState{
				Status: frontiercheckpoint.RunCompleted,
				Tally:  yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2, Duplicates: 1},
			},
			wantState:       yagocrawlcontract.CrawlRunFinished,
			wantTally:       yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2, Duplicates: 1},
			wantDisposition: "ack",
			attachTally:     true,
		},
		{
			name: "completed page failures",
			state: frontiercheckpoint.RunState{
				Status: frontiercheckpoint.RunCompleted,
				Tally:  yagocrawlcontract.CrawlRunTally{Fetched: 2, Failed: 2},
			},
			wantState:       yagocrawlcontract.CrawlRunFinished,
			wantTally:       yagocrawlcontract.CrawlRunTally{Fetched: 2, Failed: 2},
			wantDisposition: "ack",
			attachTally:     true,
		},
		{
			name: "legacy failed",
			state: frontiercheckpoint.RunState{
				Status: frontiercheckpoint.RunCompleted,
				Failed: true,
			},
			wantState:       yagocrawlcontract.CrawlRunFinished,
			wantTally:       yagocrawlcontract.CrawlRunTally{Failed: 1},
			wantDisposition: "ack",
			attachTally:     true,
		},
		{
			name: "cancelled",
			state: frontiercheckpoint.RunState{
				Status:  frontiercheckpoint.RunActive,
				Control: frontiercheckpoint.RunControl{Cancelled: true},
				Tally:   yagocrawlcontract.CrawlRunTally{Fetched: 4, RobotsDenied: 1},
			},
			wantState:       yagocrawlcontract.CrawlRunCancelled,
			wantTally:       yagocrawlcontract.CrawlRunTally{Fetched: 4, RobotsDenied: 1},
			wantDisposition: "term",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runTerminalRecoveryCase(t, test)
		})
	}
}

func runTerminalRecoveryCase(t *testing.T, test terminalRecoveryCase) {
	t.Helper()
	events := &recoveryEventLog{}
	deleted := make(chan struct{})
	checkpoint := terminalRecoveryCheckpoint{state: test.state, events: events, deleted: deleted}
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	tally := runtally.New()
	order, identity := checkpointOrder(t, "terminal-"+test.name)
	settle := func(disposition string) func(context.Context) error {
		return func(context.Context) error {
			events.append(disposition)

			return nil
		}
	}
	consumerQueue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	consumer := crawlorder.NewCrawlOrderConsumer(
		consumerQueue,
		crawlFrontier,
	).WithProgressReporter(events)
	if test.attachTally {
		consumer.WithRunTally(tally)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := consumerQueue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		LeaseID:       "lease-" + test.name,
		Order:         order,
		OrderIdentity: identity,
		Ack:           settle("ack"),
		Nak:           settle("nak"),
		Term:          settle("term"),
	}); err != nil {
		t.Fatalf("publish recovery: %v", err)
	}
	select {
	case <-deleted:
	case <-time.After(time.Second):
		t.Fatal("terminal checkpoint was not deleted")
	}
	gotEvents, reports := events.snapshot()
	wantEvents := []string{"report", test.wantDisposition, "delete"}
	if !slices.Equal(gotEvents, wantEvents) {
		t.Fatalf("events = %v, want %v", gotEvents, wantEvents)
	}
	if len(reports) != 1 || reports[0].State != test.wantState ||
		reports[0].Tally != test.wantTally {
		t.Fatalf(
			"reports = %+v, want state %v tally %+v",
			reports,
			test.wantState,
			test.wantTally,
		)
	}
	if got := tally.Snapshot(order.Provenance); got != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("retained tally = %+v, want empty after terminal report", got)
	}
}
