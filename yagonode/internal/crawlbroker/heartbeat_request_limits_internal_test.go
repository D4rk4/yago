package crawlbroker

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestHeartbeatRejectsUnboundedOrInvalidListsBeforeMutation(t *testing.T) {
	activeOverflow := make([]string, yagocrawlcontract.MaximumHeartbeatActiveLeases+1)
	for index := range activeOverflow {
		activeOverflow[index] = "lease"
	}
	acknowledgmentOverflow := make(
		[]uint64,
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments+1,
	)
	tests := []struct {
		name         string
		active       []string
		acknowledged []uint64
	}{
		{name: "active cardinality", active: activeOverflow},
		{name: "empty lease", active: []string{""}},
		{
			name: "oversized lease",
			active: []string{strings.Repeat(
				"l",
				yagocrawlcontract.MaximumCrawlLeaseIDBytes+1,
			)},
		},
		{name: "invalid lease encoding", active: []string{string([]byte{0xff})}},
		{name: "zero acknowledgment", acknowledged: []uint64{0}},
		{name: "acknowledgment cardinality", acknowledged: acknowledgmentOverflow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := memQueue(t)
			leaseID := leaseOneForSession(
				t, queue, "bounded-heartbeat", "worker", testWorkerSessionID,
			)
			server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
			activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
			before, _ := leaseRecordFor(t, queue, leaseID)
			_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
				WorkerId:                 "worker",
				WorkerSessionId:          testWorkerSessionID,
				ActiveLeaseIds:           test.active,
				AcknowledgedDirectiveIds: test.acknowledged,
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("heartbeat status = %v, want InvalidArgument", status.Code(err))
			}
			after, found := leaseRecordFor(t, queue, leaseID)
			if !found || !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid heartbeat mutated lease = %#v/%v, want %#v", after, found, before)
			}
		})
	}
}

func TestHeartbeatAcceptsExactProtocolBounds(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(
		t, queue, "boundary-heartbeat", "worker", testWorkerSessionID,
	)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	active := make([]string, yagocrawlcontract.MaximumHeartbeatActiveLeases)
	active[0] = leaseID
	for index := 1; index < len(active); index++ {
		active[index] = fmt.Sprintf("missing-%d", index)
	}
	acknowledged := make(
		[]uint64,
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments,
	)
	for index := range acknowledged {
		acknowledged[index] = uint64(index + 1)
	}
	result, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId:                 "worker",
		WorkerSessionId:          testWorkerSessionID,
		ActiveLeaseIds:           active,
		AcknowledgedDirectiveIds: acknowledged,
	})
	if err != nil {
		t.Fatalf("heartbeat at protocol bounds: %v", err)
	}
	if renewed := result.GetRenewedLeaseIds(); len(renewed) != 1 || renewed[0] != leaseID {
		t.Fatalf("renewed leases = %v, want [%s]", renewed, leaseID)
	}
}

func TestHeartbeatListNormalizationCollapsesDuplicates(t *testing.T) {
	leases, valid := normalizedHeartbeatLeaseIDs([]string{"lease-a", "lease-a", "lease-b"})
	if !valid || len(leases) != 2 || leases[0] != "lease-a" || leases[1] != "lease-b" {
		t.Fatalf("normalized leases = %v/%t", leases, valid)
	}
	acknowledged, valid := normalizedHeartbeatDirectiveAcknowledgments([]uint64{4, 4, 7})
	if !valid || len(acknowledged) != 2 || acknowledged[0] != 4 || acknowledged[1] != 7 {
		t.Fatalf("normalized acknowledgments = %v/%t", acknowledged, valid)
	}
}

func TestHeartbeatRejectsOversizedAcknowledgmentWithoutDeletingDirective(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	directive, err := server.control.directives.Enqueue(
		t.Context(),
		"worker",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlRestart},
	)
	if err != nil {
		t.Fatalf("enqueue directive: %v", err)
	}
	acknowledged := make(
		[]uint64,
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments+1,
	)
	for index := range acknowledged {
		acknowledged[index] = directive.DirectiveID
	}
	_, err = server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId:                 "worker",
		WorkerSessionId:          testWorkerSessionID,
		AcknowledgedDirectiveIds: acknowledged,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("heartbeat status = %v, want InvalidArgument", status.Code(err))
	}
	pending, err := server.control.directives.Exchange(t.Context(), "worker", nil)
	if err != nil {
		t.Fatalf("read pending directives: %v", err)
	}
	if len(pending) != 1 || pending[0].DirectiveID != directive.DirectiveID {
		t.Fatalf("pending directives = %+v, want directive %d", pending, directive.DirectiveID)
	}
}
