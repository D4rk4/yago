package worker

import (
	"context"
	"errors"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

var errInvalidRecoveredOrderFrame = errors.New("invalid recovered crawl order framing")

type recoveredReplay struct {
	leaseIDs    []string
	next        int
	remaining   map[string]struct{}
	manifestSet bool
	prefixEnded bool
}

func (worker *Worker) confirmOrderDelivery(
	ctx context.Context,
	message *crawlrpc.CrawlOrderMessage,
	replay *recoveredReplay,
) error {
	if !yagocrawlcontract.ValidCrawlLeaseID(message.GetLeaseId()) {
		return errInvalidRecoveredOrderFrame
	}
	if !message.GetRecovered() {
		if len(replay.leaseIDs) != 0 || message.GetRecoveredBatchEnd() ||
			len(message.GetRecoveredLeaseIds()) != 0 ||
			len(message.GetRecoveredSessionLeaseIds()) != 0 ||
			len(replay.remaining) > 0 {
			return errInvalidRecoveredOrderFrame
		}
		replay.prefixEnded = true
		worker.trackLease(message.GetLeaseId())
		return worker.confirmLeases(ctx, worker.activeLeaseIDs(), true)
	}
	if replay.prefixEnded {
		return errInvalidRecoveredOrderFrame
	}
	startedBatch := false
	if len(replay.leaseIDs) == 0 {
		startedBatch = true
		batchLeaseIDs := message.GetRecoveredLeaseIds()
		if !validLeaseHeader(batchLeaseIDs) {
			return errInvalidRecoveredOrderFrame
		}
		sessionLeaseIDs := message.GetRecoveredSessionLeaseIds()
		if len(sessionLeaseIDs) > 0 {
			if replay.manifestSet || !validLeaseHeader(sessionLeaseIDs) {
				return errInvalidRecoveredOrderFrame
			}
			replay.manifestSet = true
			replay.remaining = make(map[string]struct{}, len(sessionLeaseIDs))
			for _, leaseID := range sessionLeaseIDs {
				replay.remaining[leaseID] = struct{}{}
				worker.trackLease(leaseID)
			}
			if err := worker.confirmLeases(ctx, sessionLeaseIDs, false); err != nil {
				return err
			}
		} else {
			for _, leaseID := range batchLeaseIDs {
				worker.trackLease(leaseID)
			}
		}
		for _, leaseID := range batchLeaseIDs {
			if replay.manifestSet {
				if _, found := replay.remaining[leaseID]; !found {
					return errInvalidRecoveredOrderFrame
				}
				delete(replay.remaining, leaseID)
			}
		}
		if err := worker.confirmLeases(ctx, batchLeaseIDs, true); err != nil {
			return err
		}
		replay.leaseIDs = append([]string(nil), batchLeaseIDs...)
	}
	if !startedBatch && (len(message.GetRecoveredLeaseIds()) > 0 || len(message.GetRecoveredSessionLeaseIds()) > 0) ||
		replay.next >= len(replay.leaseIDs) || replay.leaseIDs[replay.next] != message.GetLeaseId() {
		return errInvalidRecoveredOrderFrame
	}
	replay.next++
	finished := replay.next == len(replay.leaseIDs)
	if message.GetRecoveredBatchEnd() != finished {
		return errInvalidRecoveredOrderFrame
	}
	if finished {
		replay.leaseIDs = nil
		replay.next = 0
	}
	return nil
}

func (worker *Worker) confirmLeases(ctx context.Context, leaseIDs []string, confirm bool) error {
	if _, err := worker.sendHeartbeat(
		ctx,
		leaseIDs,
		worker.active.Load(),
		confirm,
	); err != nil {
		return err
	}
	return nil
}

func validLeaseHeader(leaseIDs []string) bool {
	if len(leaseIDs) == 0 || len(leaseIDs) > yagocrawlcontract.MaximumHeartbeatActiveLeases {
		return false
	}
	seen := make(map[string]struct{}, len(leaseIDs))
	for _, leaseID := range leaseIDs {
		if !yagocrawlcontract.ValidCrawlLeaseID(leaseID) {
			return false
		}
		if _, duplicate := seen[leaseID]; duplicate {
			return false
		}
		seen[leaseID] = struct{}{}
	}
	return true
}
