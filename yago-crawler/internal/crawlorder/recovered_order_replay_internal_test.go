package crawlorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestRecoveredOrderReplayUsesOneBoundedHeartbeat(t *testing.T) {
	for _, size := range []int{270, yagocrawlcontract.MaximumHeartbeatActiveLeases} {
		t.Run(fmt.Sprintf("leases-%d", size), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			registry := crawllease.NewGrantRegistry(
				ctx,
				yagocrawlcontract.MaximumHeartbeatActiveLeases,
			)
			client := &fakeStreamer{
				ctx:         ctx,
				renewActive: true,
				leaseTTL:    time.Minute,
			}
			heartbeat := &heartbeatDelivery{
				client: client, workerID: "worker", workerSessionID: "session",
				acknowledgments: &controlAcknowledgments{}, leaseGrants: registry,
			}
			stream := &fakeOrderStream{ctx: ctx, results: recoveredOrderResults(t, size)}
			out := make(chan CrawlOrderDelivery, size)
			drainOrderStreamWithLeaseSession(ctx, crawlOrderStreamDrain{
				client: client, stream: stream, out: out, workerID: "worker", heartbeat: heartbeat,
			})
			if len(out) != size {
				t.Fatalf("delivered replay orders = %d, want %d", len(out), size)
			}
			requests := client.heartbeatRequests()
			if len(requests) != 1 {
				t.Fatalf("replay heartbeat calls = %d, want 1", len(requests))
			}
			leaseReads := 0
			for _, request := range requests {
				leaseReads += len(request.GetActiveLeaseIds())
			}
			if leaseReads != size {
				t.Fatalf("replay lease reads = %d, want %d", leaseReads, size)
			}
			if active := registry.ActiveLeaseIDs(); len(active) != size {
				t.Fatalf("confirmed replay grants = %d, want %d", len(active), size)
			}
		})
	}
}

func TestRecoveredOrderReplayRejectsBrokenBatchBoundaries(t *testing.T) {
	open := orderResult(t, "framing")
	open.msg.Recovered = true
	open.msg.RecoveredLeaseIds = []string{open.msg.GetLeaseId(), "lease-live"}
	for _, test := range []struct {
		name          string
		results       []recvResult
		deliveredPart int
	}{
		{
			name: "end without recovered marker",
			results: []recvResult{{msg: &crawlrpc.CrawlOrderMessage{
				RecoveredBatchEnd: true,
			}}},
		},
		{
			name: "live order interrupts recovered batch",
			results: []recvResult{
				open,
				orderResult(t, "live"),
			},
			deliveredPart: 1,
		},
		{
			name:          "stream ends inside recovered batch",
			results:       []recvResult{open, {err: io.EOF}},
			deliveredPart: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := make(chan CrawlOrderDelivery, 1)
			drainOrderStreamWithLeaseSession(t.Context(), crawlOrderStreamDrain{
				client: &fakeStreamer{ctx: t.Context()},
				stream: &fakeOrderStream{ctx: t.Context(), results: test.results},
				out:    out,
			})
			if len(out) != test.deliveredPart {
				t.Fatalf(
					"validated recovered prefix = %d, want %d",
					len(out),
					test.deliveredPart,
				)
			}
		})
	}
}

func TestRecoveredOrderReplayRejectsOversizedBatch(t *testing.T) {
	results := recoveredOrderResults(t, yagocrawlcontract.MaximumHeartbeatActiveLeases+1)
	out := make(chan CrawlOrderDelivery, len(results))
	drainOrderStreamWithLeaseSession(t.Context(), crawlOrderStreamDrain{
		client: &fakeStreamer{ctx: t.Context()},
		stream: &fakeOrderStream{ctx: t.Context(), results: results},
		out:    out,
	})
	if len(out) != 0 {
		t.Fatal("oversized recovered batch delivered an order")
	}
}

func TestRecoveredOrderReplayValidatesHeaderSequence(t *testing.T) {
	for _, test := range []struct {
		name    string
		replay  recoveredOrderReplay
		message *crawlrpc.CrawlOrderMessage
	}{
		{
			name:    "missing header",
			message: &crawlrpc.CrawlOrderMessage{LeaseId: "first"},
		},
		{
			name: "duplicate lease",
			message: &crawlrpc.CrawlOrderMessage{
				LeaseId:           "first",
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{"first", "first"},
			},
		},
		{
			name: "invalid lease",
			message: &crawlrpc.CrawlOrderMessage{
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{""},
			},
		},
		{
			name:   "repeated header",
			replay: recoveredOrderReplay{leaseIDs: []string{"first", "second"}, next: 1},
			message: &crawlrpc.CrawlOrderMessage{
				LeaseId:           "second",
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{"second"},
			},
		},
		{
			name:   "lease order mismatch",
			replay: recoveredOrderReplay{leaseIDs: []string{"first"}},
			message: &crawlrpc.CrawlOrderMessage{
				LeaseId:           "other",
				RecoveredBatchEnd: true,
			},
		},
		{
			name: "premature end",
			message: &crawlrpc.CrawlOrderMessage{
				LeaseId:           "first",
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{"first", "second"},
			},
		},
		{
			name: "missing end",
			message: &crawlrpc.CrawlOrderMessage{
				LeaseId:           "first",
				RecoveredLeaseIds: []string{"first"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := test.replay.accept(test.message)
			if !errors.Is(err, errRecoveredOrderBatchFraming) {
				t.Fatalf("framing error = %v", err)
			}
		})
	}
}

func TestRecoveredMalformedOrderConfirmsThenSettles(t *testing.T) {
	client := &fakeStreamer{
		ctx:         t.Context(),
		renewActive: true,
		leaseTTL:    time.Minute,
	}
	heartbeat := &heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     crawllease.NewGrantRegistry(t.Context(), 1),
	}
	drainOrderStreamWithLeaseSession(t.Context(), crawlOrderStreamDrain{
		client: client,
		stream: &fakeOrderStream{ctx: t.Context(), results: []recvResult{
			{msg: &crawlrpc.CrawlOrderMessage{
				OrderJson:         []byte("{"),
				LeaseId:           "malformed-recovered",
				Recovered:         true,
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{"malformed-recovered"},
			}},
			{err: io.EOF},
		}},
		out:       make(chan CrawlOrderDelivery, 1),
		workerID:  "worker",
		heartbeat: heartbeat,
	})
	if client.beatCallCount() != 1 || len(client.acknowledgementCalls()) != 1 {
		t.Fatalf(
			"malformed replay heartbeat/settlements = %d/%d",
			client.beatCallCount(),
			len(client.acknowledgementCalls()),
		)
	}
	if active := heartbeat.leaseGrants.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("malformed recovered grants = %v", active)
	}
}

func TestRecoveredMalformedSettlementFailureRevokesConfirmedGrant(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	leaseID := "malformed-recovered-failure"
	confirmTestGrant(t, registry, leaseID)
	grantContext, found := registry.Context(leaseID)
	if !found {
		t.Fatal("malformed failure grant context is missing")
	}
	client := &fakeStreamer{
		ctx:         t.Context(),
		ackErr:      status.Error(codes.InvalidArgument, "settlement rejected"),
		renewActive: true,
		leaseTTL:    time.Minute,
	}
	heartbeat := &heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
	}
	drainOrderStreamWithLeaseSession(t.Context(), crawlOrderStreamDrain{
		client: client,
		stream: &fakeOrderStream{ctx: t.Context(), results: []recvResult{{
			msg: &crawlrpc.CrawlOrderMessage{
				OrderJson:         []byte("{"),
				LeaseId:           leaseID,
				Recovered:         true,
				RecoveredBatchEnd: true,
				RecoveredLeaseIds: []string{leaseID},
			},
		}}},
		out:       make(chan CrawlOrderDelivery, 1),
		workerID:  "worker",
		heartbeat: heartbeat,
	})
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("failed malformed settlement retained grants = %v", active)
	}
	if cause := context.Cause(grantContext); !errors.Is(cause, crawllease.ErrLeaseLost) {
		t.Fatalf("failed malformed settlement grant cause = %v", cause)
	}
}

func TestRecoveredLeaseConfirmationFailureRetainsOnlyExistingGrant(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("existing"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(time.Now(), time.Hour, []string{"existing"}, []string{"existing"})
	delivery := heartbeatDelivery{
		client:          &fakeStreamer{ctx: t.Context()},
		workerID:        "worker",
		workerSessionID: "session",
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
	}
	if delivery.confirmRecoveredLeases(t.Context(), []string{"new"}) {
		t.Fatal("over-capacity recovered lease confirmation succeeded")
	}
	active := registry.ActiveLeaseIDs()
	if len(active) != 1 || active[0] != "existing" {
		t.Fatalf("active grants after capacity failure = %v", active)
	}
}

func TestRecoveredLeaseConfirmationRollsBackFailedHeartbeat(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	delivery := heartbeatDelivery{
		client:          &fakeStreamer{ctx: t.Context(), beatErr: errors.New("unavailable")},
		workerID:        "worker",
		workerSessionID: "session",
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
	}
	if delivery.confirmRecoveredLeases(t.Context(), []string{"new"}) {
		t.Fatal("failed recovered heartbeat confirmed its lease")
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("failed recovered heartbeat retained grants %v", active)
	}
}

func TestRecoveredLeaseConfirmationRequiresEveryRenewal(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	delivery := heartbeatDelivery{
		client:          &fakeStreamer{ctx: t.Context()},
		workerID:        "worker",
		workerSessionID: "session",
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
	}
	if delivery.confirmRecoveredLeases(t.Context(), []string{"omitted"}) {
		t.Fatal("omitted recovered lease was confirmed")
	}
}

func TestRecoveredLeaseConfirmationAppliesDirectivesOnce(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	handler := &recordingControlHandler{}
	client := &fakeStreamer{
		ctx:         t.Context(),
		renewActive: true,
		leaseTTL:    time.Minute,
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			DirectiveId: 89,
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		}},
	}
	delivery := heartbeatDelivery{
		client: client, workerID: "worker", workerSessionID: "session", control: handler,
		acknowledgments: &controlAcknowledgments{}, leaseGrants: registry,
	}
	if !delivery.confirmRecoveredLeases(t.Context(), []string{"directed"}) {
		t.Fatal("directed recovered lease was not confirmed")
	}
	if len(handler.snapshot()) != 1 || client.beatCallCount() != 2 {
		t.Fatalf(
			"recovered directive applications/heartbeats = %d/%d",
			len(handler.snapshot()),
			client.beatCallCount(),
		)
	}
}

func TestRecoveredLeaseConfirmationAllowsNoGrantWork(t *testing.T) {
	if !(heartbeatDelivery{}).confirmRecoveredLeases(t.Context(), []string{"legacy"}) {
		t.Fatal("legacy recovered lease required a grant")
	}
	delivery := heartbeatDelivery{leaseGrants: crawllease.NewGrantRegistry(t.Context(), 1)}
	if !delivery.confirmRecoveredLeases(t.Context(), nil) {
		t.Fatal("empty recovered lease set failed")
	}
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	delivery = heartbeatDelivery{
		client: &fakeStreamer{
			ctx:         t.Context(),
			renewActive: true,
			leaseTTL:    time.Minute,
		},
		leaseGrants: registry,
	}
	if !delivery.confirmRecoveredLeases(t.Context(), []string{"without-controls"}) {
		t.Fatal("recovered lease required control acknowledgments")
	}
}

func TestRecoveredOrderBatchStopsWhenDeliveryFails(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	result := orderResult(t, "cancelled-recovered")
	result.msg.Recovered = true
	result.msg.RecoveredBatchEnd = true
	result.msg.RecoveredLeaseIds = []string{result.msg.GetLeaseId()}
	out := make(chan CrawlOrderDelivery)
	drainOrderStreamWithLeaseSession(cancelled, crawlOrderStreamDrain{
		client: &fakeStreamer{ctx: cancelled},
		stream: &fakeOrderStream{ctx: cancelled, results: []recvResult{result}},
		out:    out,
	})
	if len(out) != 0 {
		t.Fatal("cancelled recovered stream delivered an order")
	}
}

func TestRecoveredOrderStreamStopsAfterBatchConfirmationFailure(t *testing.T) {
	result := orderResult(t, "unconfirmed-recovered")
	result.msg.Recovered = true
	result.msg.RecoveredBatchEnd = true
	result.msg.RecoveredLeaseIds = []string{result.msg.GetLeaseId()}
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	client := &fakeStreamer{ctx: t.Context()}
	out := make(chan CrawlOrderDelivery, 1)
	drainOrderStreamWithLeaseSession(t.Context(), crawlOrderStreamDrain{
		client: client,
		stream: &fakeOrderStream{ctx: t.Context(), results: []recvResult{result}},
		out:    out,
		heartbeat: &heartbeatDelivery{
			client: client, workerID: "worker", workerSessionID: "session",
			acknowledgments: &controlAcknowledgments{}, leaseGrants: registry,
		},
	})
	if len(out) != 0 {
		t.Fatal("unconfirmed recovered stream batch delivered an order")
	}
}

func recoveredOrderResults(t *testing.T, size int) []recvResult {
	t.Helper()
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: "recovered"},
		),
	}
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal recovered order: %v", err)
	}
	leaseIDs := make([]string, size)
	for index := range size {
		leaseIDs[index] = fmt.Sprintf("recovered-lease-%d", index)
	}
	results := make([]recvResult, 0, size+1)
	for index := range size {
		message := &crawlrpc.CrawlOrderMessage{
			OrderJson:         data,
			LeaseId:           leaseIDs[index],
			Recovered:         true,
			RecoveredBatchEnd: index == size-1,
		}
		if index == 0 {
			message.RecoveredLeaseIds = leaseIDs
		}
		results = append(results, recvResult{msg: message})
	}

	return append(results, recvResult{err: io.EOF})
}
