package crawlorder

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (d heartbeatDelivery) exchangeURLDenylistBootstrap(
	ctx context.Context,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	heartbeatCtx, cancelHeartbeat := boundedHeartbeatContext(ctx)
	defer cancelHeartbeat()
	result, err := d.client.Heartbeat(heartbeatCtx, &crawlrpc.WorkerHeartbeat{
		WorkerId:             d.workerID,
		WorkerSessionId:      d.workerSessionID,
		UrlDenylistRevision:  d.urlDenylist.Revision(),
		UrlDenylistBootstrap: true,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap crawler URL denylist: %w", err)
	}
	if err := d.applyURLDenylist(result); err != nil {
		return nil, err
	}

	return result, nil
}

func (d heartbeatDelivery) applyURLDenylist(
	result *crawlrpc.WorkerHeartbeatResult,
) error {
	if d.urlDenylist == nil {
		return nil
	}
	wire := result.GetUrlDenylist()
	if wire == nil {
		if d.urlDenylist.Ready() {
			return nil
		}

		return fmt.Errorf("crawler URL denylist bootstrap response is missing policy")
	}
	if err := d.urlDenylist.Apply(yagocrawlcontract.CrawlURLDenylist{
		Revision:  wire.GetRevision(),
		ExactURLs: wire.GetExactUrls(),
		Domains:   wire.GetDomains(),
	}); err != nil {
		return fmt.Errorf("apply crawler URL denylist: %w", err)
	}

	return nil
}
