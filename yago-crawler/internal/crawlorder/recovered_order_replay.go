package crawlorder

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

var (
	errRecoveredOrderBatchInterrupted = errors.New("recovered crawl order batch interrupted")
	errRecoveredOrderBatchOverflow    = errors.New(
		"recovered crawl order batch exceeds lease capacity",
	)
	errRecoveredOrderBatchFraming = errors.New("recovered crawl order batch framing is invalid")
)

type recoveredOrderReplay struct {
	leaseIDs []string
	next     int
}

func drainCrawlOrderMessages(ctx context.Context, drain crawlOrderStreamDrain) {
	var recovered recoveredOrderReplay
	for {
		message, err := drain.stream.Recv()
		if err != nil {
			logOrderStreamEnd(ctx, err, recovered.active())

			return
		}
		if message.GetRecovered() {
			if !receiveRecoveredOrder(ctx, drain, &recovered, message) {
				return
			}
			continue
		}
		if !receiveOrdinaryOrder(ctx, drain, recovered, message) {
			return
		}
	}
}

func receiveOrdinaryOrder(
	ctx context.Context,
	drain crawlOrderStreamDrain,
	recovered recoveredOrderReplay,
	message *crawlrpc.CrawlOrderMessage,
) bool {
	if recovered.active() || message.GetRecoveredBatchEnd() ||
		len(message.GetRecoveredLeaseIds()) > 0 {
		slog.WarnContext(
			ctx,
			msgOrderStreamReceiveFailed,
			slog.Any("error", errRecoveredOrderBatchFraming),
		)

		return false
	}
	delivery, valid := decodeCrawlOrderMessage(ctx, drain, message)

	return !valid || confirmAndDeliverCrawlOrder(ctx, drain, delivery)
}

func receiveRecoveredOrder(
	ctx context.Context,
	drain crawlOrderStreamDrain,
	recovered *recoveredOrderReplay,
	message *crawlrpc.CrawlOrderMessage,
) bool {
	started, finished, err := recovered.accept(message)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgOrderStreamReceiveFailed,
			slog.Any("error", err),
		)

		return false
	}
	if started && drain.heartbeat != nil &&
		!drain.heartbeat.confirmRecoveredLeases(ctx, recovered.leaseIDs) {
		return false
	}
	delivery, valid := decodeCrawlOrderMessage(ctx, drain, message)
	if valid && !deliverOrderWithLeaseSession(ctx, delivery) {
		return false
	}
	if finished {
		*recovered = recoveredOrderReplay{}
	}

	return true
}

func (r *recoveredOrderReplay) accept(
	message *crawlrpc.CrawlOrderMessage,
) (bool, bool, error) {
	started := false
	if !r.active() {
		leaseIDs := message.GetRecoveredLeaseIds()
		if len(leaseIDs) == 0 {
			return false, false, errRecoveredOrderBatchFraming
		}
		if len(leaseIDs) > yagocrawlcontract.MaximumHeartbeatActiveLeases {
			return false, false, errRecoveredOrderBatchOverflow
		}
		if !validRecoveredLeaseHeader(leaseIDs) {
			return false, false, errRecoveredOrderBatchFraming
		}
		r.leaseIDs = append([]string(nil), leaseIDs...)
		started = true
	} else if len(message.GetRecoveredLeaseIds()) > 0 {
		return false, false, errRecoveredOrderBatchFraming
	}
	if r.next >= len(r.leaseIDs) || r.leaseIDs[r.next] != message.GetLeaseId() {
		return false, false, errRecoveredOrderBatchFraming
	}
	r.next++
	finished := r.next == len(r.leaseIDs)
	if message.GetRecoveredBatchEnd() != finished {
		return false, false, errRecoveredOrderBatchFraming
	}

	return started, finished, nil
}

func validRecoveredLeaseHeader(leaseIDs []string) bool {
	seen := make(map[string]struct{}, len(leaseIDs))
	for _, leaseID := range leaseIDs {
		if !yagocrawlcontract.ValidCrawlLeaseID(leaseID) {
			return false
		}
		if _, exists := seen[leaseID]; exists {
			return false
		}
		seen[leaseID] = struct{}{}
	}

	return true
}

func (r recoveredOrderReplay) active() bool {
	return len(r.leaseIDs) > 0
}

func logOrderStreamEnd(ctx context.Context, err error, recoveredBatchOpen bool) {
	if recoveredBatchOpen && ctx.Err() == nil {
		slog.WarnContext(
			ctx,
			msgOrderStreamReceiveFailed,
			slog.Any("error", errRecoveredOrderBatchInterrupted),
		)

		return
	}
	if ctx.Err() == nil && !errors.Is(err, io.EOF) &&
		status.Code(err) != codes.Canceled && status.Code(err) != codes.DeadlineExceeded {
		slog.WarnContext(ctx, msgOrderStreamReceiveFailed, slog.Any("error", err))
	}
}

func decodeCrawlOrderMessage(
	ctx context.Context,
	drain crawlOrderStreamDrain,
	message *crawlrpc.CrawlOrderMessage,
) (crawlOrderDeliveryEnvelope, bool) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(message.GetOrderJson())
	if err != nil {
		slog.WarnContext(ctx, msgOrderDecodeFailed, slog.Any("error", err))
		workerSessionID := ""
		if drain.heartbeat != nil {
			workerSessionID = drain.heartbeat.workerSessionID
		}
		settleMalformedOrderForSession(
			ctx,
			drain.client,
			message.GetLeaseId(),
			drain.workerID,
			workerSessionID,
		)
		if drain.heartbeat != nil && drain.heartbeat.leaseGrants != nil {
			drain.heartbeat.leaseGrants.Revoke(message.GetLeaseId())
		}

		return crawlOrderDeliveryEnvelope{}, false
	}

	return crawlOrderDeliveryEnvelope{
		client:              drain.client,
		out:                 drain.out,
		order:               order,
		orderIdentity:       crawlOrderPayloadIdentity(message.GetOrderJson()),
		leaseID:             message.GetLeaseId(),
		workerID:            drain.workerID,
		terminalSettlements: drain.terminalSettlements,
		heartbeat:           drain.heartbeat,
	}, true
}

func confirmAndDeliverCrawlOrder(
	ctx context.Context,
	drain crawlOrderStreamDrain,
	delivery crawlOrderDeliveryEnvelope,
) bool {
	if drain.heartbeat != nil && !drain.heartbeat.confirmLease(ctx, delivery.leaseID) {
		return false
	}

	return deliverOrderWithLeaseSession(ctx, delivery)
}
