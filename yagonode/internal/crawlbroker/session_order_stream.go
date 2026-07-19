package crawlbroker

import (
	"context"
	"fmt"
)

func (s *exchangeServer) activateWorkerSession(
	ctx context.Context,
	workerID string,
	workerSessionID string,
	cancel context.CancelFunc,
	fetchStartLeaseValues ...bool,
) ([]leasedCrawlOrder, uint64, error) {
	var leased []leasedCrawlOrder
	generation, err := s.sessions.activate(workerID, workerSessionID, cancel, func() error {
		var err error
		leased, err = s.queue.adoptWorkerSession(ctx, workerID, workerSessionID)

		return err
	}, fetchStartLeaseValues...)
	if err != nil {
		return nil, 0, err
	}
	s.fetchPolicy.Lock()
	defer s.fetchPolicy.Unlock()
	if s.fetchStarts == nil {
		s.sessions.deactivate(workerID, workerSessionID, generation)

		return nil, 0, errFleetFetchPolicyInvalid
	}
	fetchStartLeases := len(fetchStartLeaseValues) > 0 && fetchStartLeaseValues[0]
	if s.fetchStarts.Snapshot().PagesPerSecond > 0 && !fetchStartLeases {
		s.sessions.deactivate(workerID, workerSessionID, generation)

		return nil, 0, errFleetFetchCapabilityRequired
	}
	s.fetchStarts.replaceSession(workerID, workerSessionID)

	return leased, generation, nil
}

func (s *exchangeServer) leaseNextForSession(
	ctx context.Context,
	workerID string,
	workerSessionID string,
	generation uint64,
) ([]byte, string, error) {
	for {
		var data []byte
		var leaseID string
		found := false
		err := s.sessions.whileCurrentRegistration(
			workerID,
			workerSessionID,
			generation,
			func() error {
				var err error
				data, leaseID, found, err = s.queue.leasePopForSession(
					ctx,
					workerID,
					workerSessionID,
				)

				return err
			},
		)
		if err != nil {
			return nil, "", err
		}
		if found {
			return data, leaseID, nil
		}
		beforeQueueWait()
		select {
		case <-s.queue.notify:
		case <-ctx.Done():
			return nil, "", fmt.Errorf("await crawl order: %w", ctx.Err())
		}
	}
}
