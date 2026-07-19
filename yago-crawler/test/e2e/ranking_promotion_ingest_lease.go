//go:build e2e

package e2e

import (
	"context"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	rankingPromotionWorkerID  = "ranking-promotion-e2e"
	rankingPromotionSessionID = "ranking-promotion-e2e-session"
)

type rankingPromotionLease struct {
	client     crawlrpc.CrawlExchangeClient
	stream     crawlrpc.CrawlExchange_StreamOrdersClient
	leaseID    string
	provenance []byte
}

func claimRankingPromotionLease(
	t *testing.T,
	ctx context.Context,
	client crawlrpc.CrawlExchangeClient,
	profileHandle string,
) rankingPromotionLease {
	t.Helper()
	stream, err := client.StreamOrders(ctx, &crawlrpc.WorkerRegistration{
		WorkerId:         rankingPromotionWorkerID,
		WorkerSessionId:  rankingPromotionSessionID,
		FetchStartLeases: true,
	})
	if err != nil {
		t.Fatalf("stream ranking crawl order: %v", err)
	}
	message, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive ranking crawl order: %v", err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(message.GetOrderJson())
	if err != nil {
		t.Fatalf("decode ranking crawl order: %v", err)
	}
	if order.Profile.Handle != profileHandle {
		t.Fatalf("ranking crawl profile = %q, want %q", order.Profile.Handle, profileHandle)
	}
	if message.GetLeaseId() == "" || len(order.Provenance) == 0 {
		t.Fatal("ranking crawl order has an empty lease identity")
	}

	return rankingPromotionLease{
		client:     client,
		stream:     stream,
		leaseID:    message.GetLeaseId(),
		provenance: append([]byte(nil), order.Provenance...),
	}
}

func (lease rankingPromotionLease) renew(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := lease.stream.Context().Err(); err != nil {
		t.Fatalf("ranking crawl order stream: %v", err)
	}
	result, err := lease.client.Heartbeat(ctx, &crawlrpc.WorkerHeartbeat{
		WorkerId:        rankingPromotionWorkerID,
		WorkerSessionId: rankingPromotionSessionID,
		ActiveLeaseIds:  []string{lease.leaseID},
	})
	if err != nil {
		t.Fatalf("renew ranking crawl lease: %v", err)
	}
	if !slices.Contains(result.GetRenewedLeaseIds(), lease.leaseID) ||
		result.GetLeaseTtlMilliseconds() == 0 {
		t.Fatalf("ranking crawl lease was not renewed: %+v", result)
	}
}

func (lease rankingPromotionLease) settle(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := lease.client.AckOrder(ctx, &crawlrpc.OrderAck{
		LeaseId:         lease.leaseID,
		WorkerId:        rankingPromotionWorkerID,
		WorkerSessionId: rankingPromotionSessionID,
	})
	if err != nil {
		t.Fatalf("settle ranking crawl order: %v", err)
	}
}
