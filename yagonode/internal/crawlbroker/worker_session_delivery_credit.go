package crawlbroker

import (
	"context"
	"fmt"
	"sync"
)

type crawlOrderDeliveryConfirmation struct {
	expectedLeaseIDs map[string]struct{}
	confirmed        chan struct{}
	stopped          chan struct{}
}

type workerSessionDeliveryCredit struct {
	mutex   sync.Mutex
	pending *crawlOrderDeliveryConfirmation
	stopped bool
}

var beforeDeliveryConfirmationWait = func() {}

func newWorkerSessionDeliveryCredit() *workerSessionDeliveryCredit {
	return &workerSessionDeliveryCredit{}
}

func (c *workerSessionDeliveryCredit) expect(
	leaseIDs []string,
) (*crawlOrderDeliveryConfirmation, error) {
	if len(leaseIDs) == 0 {
		return nil, fmt.Errorf("expect crawl order delivery: empty lease identities")
	}
	expected := make(map[string]struct{}, len(leaseIDs))
	for _, leaseID := range leaseIDs {
		expected[leaseID] = struct{}{}
	}
	confirmation := &crawlOrderDeliveryConfirmation{
		expectedLeaseIDs: expected,
		confirmed:        make(chan struct{}),
		stopped:          make(chan struct{}),
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.stopped {
		return nil, errLeaseLost
	}
	if c.pending != nil {
		return nil, fmt.Errorf("expect crawl order delivery: confirmation already pending")
	}
	c.pending = confirmation

	return confirmation, nil
}

func (c *workerSessionDeliveryCredit) confirm(renewedLeaseIDs []string) {
	c.confirmRenewed(renewedLeaseIDs, false)
}

func (c *workerSessionDeliveryCredit) confirmExact(renewedLeaseIDs []string) {
	c.confirmRenewed(renewedLeaseIDs, true)
}

func (c *workerSessionDeliveryCredit) confirmRenewed(
	renewedLeaseIDs []string,
	exact bool,
) {
	renewed := make(map[string]struct{}, len(renewedLeaseIDs))
	for _, leaseID := range renewedLeaseIDs {
		renewed[leaseID] = struct{}{}
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.pending == nil {
		return
	}
	if exact && len(renewed) != len(c.pending.expectedLeaseIDs) {
		return
	}
	for leaseID := range c.pending.expectedLeaseIDs {
		if _, found := renewed[leaseID]; !found {
			return
		}
	}
	confirmation := c.pending
	c.pending = nil
	close(confirmation.confirmed)
}

func (c *workerSessionDeliveryCredit) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true
	if c.pending == nil {
		return
	}
	confirmation := c.pending
	c.pending = nil
	close(confirmation.stopped)
}

func (c *crawlOrderDeliveryConfirmation) wait(ctx context.Context) error {
	beforeDeliveryConfirmationWait()
	select {
	case <-c.confirmed:
		return nil
	case <-c.stopped:
		return errLeaseLost
	case <-ctx.Done():
		return fmt.Errorf("await crawl order delivery confirmation: %w", ctx.Err())
	}
}

func (r *workerSessionRegistry) expectDeliveryConfirmation(
	workerID string,
	workerSessionID string,
	generation uint64,
	leaseIDs []string,
) (*crawlOrderDeliveryConfirmation, error) {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return nil, errLeaseLost
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	current := entry.current
	if current.id != workerSessionID || current.generation != generation ||
		!current.connected || current.deliveryCredit == nil {
		entry.mutex.Unlock()

		return nil, errLeaseLost
	}
	credit := current.deliveryCredit
	entry.mutex.Unlock()

	return credit.expect(leaseIDs)
}

func (r *workerSessionRegistry) confirmDeliveries(
	workerID string,
	workerSessionID string,
	renewedLeaseIDs []string,
) {
	r.confirmDeliveriesWithMode(workerID, workerSessionID, renewedLeaseIDs, false)
}

func (r *workerSessionRegistry) confirmExactDeliveries(
	workerID string,
	workerSessionID string,
	renewedLeaseIDs []string,
) {
	r.confirmDeliveriesWithMode(workerID, workerSessionID, renewedLeaseIDs, true)
}

func (r *workerSessionRegistry) confirmDeliveriesWithMode(
	workerID string,
	workerSessionID string,
	renewedLeaseIDs []string,
	exact bool,
) {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	current := entry.current
	if current.id != workerSessionID || !current.connected || current.deliveryCredit == nil {
		entry.mutex.Unlock()

		return
	}
	credit := current.deliveryCredit
	entry.mutex.Unlock()
	credit.confirmRenewed(renewedLeaseIDs, exact)
}
