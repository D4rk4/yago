package crawlorder

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type urlDenylistHeartbeatClient struct {
	*fakeStreamer
	mu       sync.Mutex
	policy   yagocrawlcontract.CrawlURLDenylist
	requests []*crawlrpc.WorkerHeartbeat
	allow    <-chan struct{}
	corrupt  bool
}

func (c *urlDenylistHeartbeatClient) Heartbeat(
	ctx context.Context,
	request *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	if c.allow != nil {
		select {
		case <-c.allow:
		case <-ctx.Done():
			return nil, status.Error(codes.Unavailable, ctx.Err().Error())
		default:
			return nil, status.Error(codes.Unavailable, "policy unavailable")
		}
	}
	c.mu.Lock()
	c.requests = append(c.requests, request)
	policy := c.policy
	corrupt := c.corrupt
	c.mu.Unlock()
	if bytes.Equal(request.GetUrlDenylistRevision(), policy.Revision) {
		return &crawlrpc.WorkerHeartbeatResult{}, nil
	}
	revision := append([]byte(nil), policy.Revision...)
	if corrupt {
		revision[0] ^= 0xff
	}

	return &crawlrpc.WorkerHeartbeatResult{UrlDenylist: &crawlrpc.CrawlURLDenylist{
		Revision: revision, ExactUrls: policy.ExactURLs, Domains: policy.Domains,
	}}, nil
}

func (c *urlDenylistHeartbeatClient) setPolicy(
	policy yagocrawlcontract.CrawlURLDenylist,
	corrupt bool,
) {
	c.mu.Lock()
	c.policy = policy
	c.corrupt = corrupt
	c.mu.Unlock()
}

func (c *urlDenylistHeartbeatClient) heartbeatRequests() []*crawlrpc.WorkerHeartbeat {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]*crawlrpc.WorkerHeartbeat(nil), c.requests...)
}

func TestHeartbeatBootstrapsAndRevisesURLDenylist(t *testing.T) {
	first := buildURLDenylistPolicy(t, nil, []string{"first.example"})
	client := &urlDenylistHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		policy:       first,
	}
	denylist := crawldenylist.New()
	delivery := heartbeatDelivery{
		client: client, workerID: "worker", workerSessionID: "session",
		urlDenylist: denylist,
	}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("bootstrap exchange: %v", err)
	}
	requests := client.heartbeatRequests()
	if len(requests) != 1 || !requests[0].GetUrlDenylistBootstrap() ||
		len(requests[0].GetUrlDenylistRevision()) != 0 ||
		!denylist.Blocks("https://first.example/page") {
		t.Fatalf("bootstrap request/policy = %+v/%x", requests, denylist.Revision())
	}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("unchanged exchange: %v", err)
	}
	requests = client.heartbeatRequests()
	if len(requests) != 2 || requests[1].GetUrlDenylistBootstrap() ||
		!bytes.Equal(requests[1].GetUrlDenylistRevision(), first.Revision) {
		t.Fatalf("steady-state request = %+v", requests[1])
	}
	second := buildURLDenylistPolicy(t, nil, []string{"second.example"})
	client.setPolicy(second, false)
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("revised exchange: %v", err)
	}
	if denylist.Blocks("https://first.example/page") ||
		!denylist.Blocks("https://second.example/page") {
		t.Fatal("revised policy was not applied")
	}
	client.setPolicy(first, true)
	if _, err := delivery.exchange(t.Context(), nil); err == nil {
		t.Fatal("corrupt revision accepted")
	}
	if !bytes.Equal(denylist.Revision(), second.Revision) ||
		!denylist.Blocks("https://second.example/page") {
		t.Fatal("corrupt update replaced the last-good policy")
	}
}

func TestOrderStreamWaitsForInitialURLDenylist(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	allow := make(chan struct{})
	streamed := make(chan struct{}, 1)
	client := &urlDenylistHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: ctx, onStream: func() {
			streamed <- struct{}{}
		}},
		policy: buildURLDenylistPolicy(t, nil, nil),
		allow:  allow,
	}
	denylist := crawldenylist.New()
	receiver := NewGRPCOrderReceiver(
		ctx, client, "worker", nil, WithURLDenylist(denylist),
	)
	select {
	case <-streamed:
		t.Fatal("order stream opened before initial URL denylist")
	case <-time.After(25 * time.Millisecond):
	}
	close(allow)
	select {
	case <-streamed:
	case <-time.After(time.Second):
		t.Fatal("order stream did not open after URL denylist bootstrap")
	}
	if !denylist.Ready() {
		t.Fatal("URL denylist not ready after bootstrap")
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestURLDenylistBootstrapRejectsMissingPolicy(t *testing.T) {
	denylist := crawldenylist.New()
	delivery := heartbeatDelivery{
		client: &fakeStreamer{ctx: t.Context()}, workerID: "worker",
		workerSessionID: "session", urlDenylist: denylist,
	}
	if _, err := delivery.exchange(t.Context(), nil); err == nil {
		t.Fatal("missing initial policy accepted")
	}
	if denylist.Ready() {
		t.Fatal("missing policy marked denylist ready")
	}
}

func TestOrderStreamStopsWhenURLDenylistNeverInitializes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx: ctx, beatErr: status.Error(codes.Unavailable, "policy unavailable"),
	}
	receiver := NewGRPCOrderReceiver(
		ctx, client, "worker", nil, WithURLDenylist(crawldenylist.New()),
	)
	cancel()
	drainUntilClosed(t, receiver)
	if len(client.workerRegistrations()) != 0 {
		t.Fatal("order stream opened without an initial URL denylist")
	}
}

func buildURLDenylistPolicy(
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

var _ OrderStreamer = (*urlDenylistHeartbeatClient)(nil)
