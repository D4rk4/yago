package crawlbroker

import "context"

func (s *exchangeServer) acknowledgeOrder(ctx context.Context, leaseID string) error {
	target, err := s.queue.ackLeaseWithTarget(ctx, leaseID)
	if err != nil {
		return err
	}
	if target.RunID == "" {
		return nil
	}
	if err := s.control.CompleteRun(ctx, target); err != nil {
		return err
	}

	return s.queue.completeRunControl(ctx, leaseID)
}

func (s *exchangeServer) acknowledgeOrderForOwner(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
) error {
	target, err := s.queue.ackLeaseWithOwner(ctx, leaseID, workerID, workerSessionID)
	if err != nil {
		return err
	}
	if target.RunID == "" {
		return nil
	}
	if err := s.control.CompleteRun(ctx, target); err != nil {
		return err
	}

	return s.queue.completeRunControl(ctx, leaseID)
}
