package crawlorder

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (d heartbeatDelivery) exchangeForLeases(
	ctx context.Context,
	acknowledged []uint64,
	activeLeaseIDs []string,
	confirmDeliveries bool,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	if d.urlDenylist != nil && !d.urlDenylist.Ready() {
		return d.exchangeURLDenylistBootstrap(ctx)
	}
	requestStarted := time.Now()
	heartbeatCtx, cancelHeartbeat := boundedHeartbeatContext(ctx)
	defer cancelHeartbeat()
	heartbeat := d.leaseHeartbeat(activeLeaseIDs, acknowledged, confirmDeliveries)
	result, err := d.client.Heartbeat(heartbeatCtx, heartbeat)
	if err != nil {
		d.rejectLeasesAfterHeartbeatError(err)

		return nil, fmt.Errorf("deliver crawler heartbeat: %w", err)
	}
	if err := d.applyURLDenylist(result); err != nil {
		return nil, err
	}
	if err := d.applyHeartbeatRuntimePolicy(result); err != nil {
		return nil, err
	}
	d.applyHeartbeatStoragePolicy(result)
	if err := d.renewHeartbeatLeases(requestStarted, activeLeaseIDs, result); err != nil {
		return nil, err
	}

	return result, nil
}

func heartbeatLeaseTTL(milliseconds uint64) (time.Duration, error) {
	maximumMilliseconds := uint64(math.MaxInt64 / int64(time.Millisecond))
	if milliseconds > maximumMilliseconds {
		return 0, fmt.Errorf("deliver crawler heartbeat: lease duration is out of range")
	}

	return time.Duration(milliseconds) * time.Millisecond, nil
}
