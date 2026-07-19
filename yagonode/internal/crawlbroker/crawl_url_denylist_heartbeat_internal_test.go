package crawlbroker

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestURLDenylistBootstrapPrecedesWorkerSession(t *testing.T) {
	policy := testCrawlURLDenylist(
		t,
		[]string{"https://exact.example/"},
		[]string{"blocked.example"},
	)
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.urlDenylist.SetSource(func() (yagocrawlcontract.CrawlURLDenylist, error) {
		return policy, nil
	})
	result, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		UrlDenylistBootstrap: true,
	})
	if err != nil {
		t.Fatalf("bootstrap heartbeat: %v", err)
	}
	wire := result.GetUrlDenylist()
	if wire == nil || !bytes.Equal(wire.GetRevision(), policy.Revision) ||
		!reflect.DeepEqual(wire.GetExactUrls(), policy.ExactURLs) ||
		!reflect.DeepEqual(wire.GetDomains(), policy.Domains) {
		t.Fatalf("bootstrap policy = %+v", wire)
	}
	unchanged, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		UrlDenylistBootstrap: true, UrlDenylistRevision: policy.Revision,
	})
	if err != nil || unchanged.GetUrlDenylist() != nil {
		t.Fatalf("unchanged bootstrap = %+v, %v", unchanged, err)
	}
}

func TestCrawlBrokerSetsURLDenylistSource(t *testing.T) {
	policy := testCrawlURLDenylist(t, nil, []string{"blocked.example"})
	delivery := newCrawlURLDenylistDelivery()
	broker := &CrawlBroker{urlDenylist: delivery}
	broker.SetURLDenylistSource(func() (yagocrawlcontract.CrawlURLDenylist, error) {
		return policy, nil
	})
	wire, err := delivery.Snapshot(nil)
	if err != nil || wire == nil || !bytes.Equal(wire.GetRevision(), policy.Revision) {
		t.Fatalf("broker policy = %+v, %v", wire, err)
	}
}

func TestURLDenylistBootstrapRejectsStateMutation(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	active := uint32(1)
	_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		UrlDenylistBootstrap: true, ActiveFetches: &active,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bootstrap status = %v, want InvalidArgument", status.Code(err))
	}
}

func TestURLDenylistRevisionIsCheckedBeforeLeaseMutation(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(
		t, queue, "denylist-heartbeat", "worker", testWorkerSessionID,
	)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	before, _ := leaseRecordFor(t, queue, leaseID)
	_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		ActiveLeaseIds: []string{leaseID}, UrlDenylistRevision: []byte("invalid"),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("heartbeat status = %v, want InvalidArgument", status.Code(err))
	}
	after, found := leaseRecordFor(t, queue, leaseID)
	if !found || !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid revision mutated lease = %#v/%v, want %#v", after, found, before)
	}
}

func TestURLDenylistNormalHeartbeatReceivesChangedRevision(t *testing.T) {
	first := testCrawlURLDenylist(t, nil, []string{"first.example"})
	second := testCrawlURLDenylist(t, nil, []string{"second.example"})
	current := first
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.urlDenylist.SetSource(func() (yagocrawlcontract.CrawlURLDenylist, error) {
		return current, nil
	})
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	current = second
	result, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		UrlDenylistRevision: first.Revision,
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if result.GetUrlDenylist() == nil ||
		!bytes.Equal(result.GetUrlDenylist().GetRevision(), second.Revision) {
		t.Fatalf("changed policy = %+v", result.GetUrlDenylist())
	}
}

func TestURLDenylistBootstrapFailsClosedWithoutValidSource(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.urlDenylist.SetSource(nil)
	request := &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		UrlDenylistBootstrap: true,
	}
	_, err := server.Heartbeat(t.Context(), request)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("unavailable source status = %v", status.Code(err))
	}
	invalid := testCrawlURLDenylist(t, nil, []string{"invalid.example"})
	invalid.Revision[0] ^= 0xff
	server.urlDenylist.SetSource(func() (yagocrawlcontract.CrawlURLDenylist, error) {
		return invalid, nil
	})
	_, err = server.Heartbeat(t.Context(), request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("invalid source status = %v", status.Code(err))
	}
	server.urlDenylist.SetSource(func() (yagocrawlcontract.CrawlURLDenylist, error) {
		return yagocrawlcontract.CrawlURLDenylist{}, errors.New("snapshot failed")
	})
	_, err = server.Heartbeat(t.Context(), request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("failed source status = %v", status.Code(err))
	}
}

func testCrawlURLDenylist(
	t *testing.T,
	exactURLs []string,
	domains []string,
) yagocrawlcontract.CrawlURLDenylist {
	t.Helper()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(exactURLs, domains)
	if err != nil {
		t.Fatalf("NewCrawlURLDenylist: %v", err)
	}

	return policy
}
