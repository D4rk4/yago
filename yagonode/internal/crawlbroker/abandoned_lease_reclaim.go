package crawlbroker

import (
	"context"
	"log/slog"
	"time"
)

// crawlLeaseAbandonMultiple sets how far past expiry a lease that still names a
// worker session must fall before the queue reclaims it anyway. Checkpoint
// affinity deliberately holds such a lease for its own worker to resume, and
// ordinary restarts re-adopt the session well inside one TTL, so the deadline is
// generous: it exists only for the case where that worker never returns.
const crawlLeaseAbandonMultiple = 8

const msgCrawlLeaseAbandoned = "crawl lease reclaimed from an absent worker"

// reclaimAbandonedLeases requeues leases whose worker never came back.
//
// Expiry alone does not release a lease that retains checkpoint affinity — one
// naming a live worker id and session — because that worker holds the frontier
// checkpoint for the run and should resume its own work when it reconnects.
// Recovery therefore runs through worker-session adoption, which needs the same
// worker id, and that id lives in the crawler's frontier database. A crawler
// whose data directory is lost (a container recreated without a persistent
// volume) comes back under a new id and can never adopt the old session, so
// without this pass the lease stays outstanding forever: the order is never
// re-executed, and an automatic-discovery order keeps its URL identity, so that
// URL can never be seeded again.
//
// This pass is deliberately disjoint from the ordinary expiry sweep. It matches
// only leases the sweep skips, so the affinity rule keeps its exact meaning for
// every lease inside the deadline.
func (q *DurableOrderQueue) reclaimAbandonedLeases(ctx context.Context) error {
	deadline := q.leaseAbandonAfter()
	now := nowFunc()
	abandoned := 0
	if err := q.requeueLeasesMatching(ctx, func(record leaseRecord) bool {
		if !leaseRetainsCheckpointAffinity(record) {
			return false
		}
		expired := now.UnixNano() - record.ExpiresAtUnixNano
		if expired < deadline.Nanoseconds() {
			return false
		}
		abandoned++

		return true
	}); err != nil {
		return err
	}
	if abandoned > 0 {
		slog.WarnContext(
			ctx,
			msgCrawlLeaseAbandoned,
			slog.Int("leases", abandoned),
			slog.String("deadline", deadline.String()),
		)
	}

	return nil
}

func (q *DurableOrderQueue) leaseAbandonAfter() time.Duration {
	return q.leaseTTL * crawlLeaseAbandonMultiple
}
