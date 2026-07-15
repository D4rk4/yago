package crawlorder

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	defaultProgressReportTimeout = 5 * time.Second
	progressPendingCapacity      = 4096
	progressRetryMinimum         = 100 * time.Millisecond
	progressRetryMaximum         = 5 * time.Second
	msgProgressReportFailed      = "crawl progress report failed"
	msgProgressReportDropped     = "crawl progress report queue full"
)

type progressDeliveryPolicy struct {
	capacity     int
	rpcTimeout   time.Duration
	retryMinimum time.Duration
	retryMaximum time.Duration
	entropy      io.Reader
}

type progressDelivery struct {
	report    RunReport
	due       time.Time
	retryWait time.Duration
	version   uint64
	terminal  bool
	warned    bool
}

type progressDeliveryQueue struct {
	client   ProgressClient
	workerID string
	policy   progressDeliveryPolicy

	mu      sync.Mutex
	pending map[string][]progressDelivery
	queued  int
	version uint64
	closed  bool
	signal  chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
}

func defaultProgressDeliveryPolicy() progressDeliveryPolicy {
	return progressDeliveryPolicy{
		capacity:     progressPendingCapacity,
		rpcTimeout:   defaultProgressReportTimeout,
		retryMinimum: progressRetryMinimum,
		retryMaximum: progressRetryMaximum,
		entropy:      cryptorand.Reader,
	}
}

func newProgressDeliveryQueue(
	client ProgressClient,
	workerID string,
	policy progressDeliveryPolicy,
) *progressDeliveryQueue {
	workerCtx, cancel := context.WithCancel(context.Background())
	queue := &progressDeliveryQueue{
		client:   client,
		workerID: workerID,
		policy:   policy,
		pending:  make(map[string][]progressDelivery, policy.capacity),
		signal:   make(chan struct{}, 1),
		done:     make(chan struct{}),
		cancel:   cancel,
	}
	go queue.run(workerCtx)

	return queue
}

func (q *progressDeliveryQueue) enqueue(ctx context.Context, report RunReport) {
	report.Provenance = append([]byte(nil), report.Provenance...)
	key := string(report.Provenance)
	terminal := terminalProgressState(report.State)
	dropped := false

	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()

		return
	}
	sequence := q.pending[key]
	if len(sequence) > 0 && sequence[len(sequence)-1].terminal == terminal {
		q.version++
		sequence[len(sequence)-1] = progressDelivery{
			report:    report,
			due:       time.Now(),
			retryWait: q.policy.retryMinimum,
			version:   q.version,
			terminal:  terminal,
		}
		q.pending[key] = sequence
		q.signalLocked()
		q.mu.Unlock()

		return
	}
	if q.queued >= q.policy.capacity {
		if terminal {
			q.evictExpendableRunningTailLocked()
		}
		if q.queued >= q.policy.capacity {
			dropped = true
		}
	}
	if !dropped {
		q.version++
		q.pending[key] = append(q.pending[key], progressDelivery{
			report:    report,
			due:       time.Now(),
			retryWait: q.policy.retryMinimum,
			version:   q.version,
			terminal:  terminal,
		})
		q.queued++
		q.signalLocked()
	}
	q.mu.Unlock()

	if dropped {
		slog.WarnContext(ctx, msgProgressReportDropped,
			slog.String("runId", key),
			slog.String("state", string(report.State)),
			slog.String("workerId", q.workerID))
	}
}

func (q *progressDeliveryQueue) close(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		for key, sequence := range q.pending {
			retained := sequence[:0]
			for index, delivery := range sequence {
				if delivery.terminal || index+1 < len(sequence) && sequence[index+1].terminal {
					retained = append(retained, delivery)
				} else {
					q.queued--
				}
			}
			if len(retained) == 0 {
				delete(q.pending, key)
			} else {
				q.pending[key] = retained
			}
		}
		q.signalLocked()
	}
	q.mu.Unlock()

	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		q.cancel()

		return fmt.Errorf("close progress delivery: %w", ctx.Err())
	}
}

func (q *progressDeliveryQueue) run(ctx context.Context) {
	defer close(q.done)
	for {
		delivery, wait, ready, finished := q.next()
		if finished {
			return
		}
		if !ready {
			select {
			case <-q.signal:
			case <-ctx.Done():
				return
			}

			continue
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-q.signal:
				stopProgressTimer(timer)
			case <-ctx.Done():
				stopProgressTimer(timer)
				return
			}
			continue
		}
		q.deliver(ctx, delivery)
	}
}

func stopProgressTimer(timer *time.Timer) {
	timer.Stop()
}

func (q *progressDeliveryQueue) next() (progressDelivery, time.Duration, bool, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		return progressDelivery{}, 0, false, q.closed
	}
	now := time.Now()
	var readyTerminal progressDelivery
	var readyRunning progressDelivery
	var future progressDelivery
	foundTerminal := false
	foundRunning := false
	foundFuture := false
	for _, sequence := range q.pending {
		delivery := sequence[0]
		if delivery.due.After(now) {
			if !foundFuture || progressDeliveryBefore(delivery, future) {
				future = delivery
				foundFuture = true
			}

			continue
		}
		if delivery.terminal {
			if !foundTerminal || progressDeliveryBefore(delivery, readyTerminal) {
				readyTerminal = delivery
				foundTerminal = true
			}
		} else if !foundRunning || progressDeliveryBefore(delivery, readyRunning) {
			readyRunning = delivery
			foundRunning = true
		}
	}
	if foundTerminal {
		return readyTerminal, 0, true, false
	}
	if foundRunning {
		return readyRunning, 0, true, false
	}

	return future, time.Until(future.due), true, false
}

func progressDeliveryBefore(left, right progressDelivery) bool {
	return left.due.Before(right.due) ||
		left.due.Equal(right.due) && left.version < right.version
}

func (q *progressDeliveryQueue) deliver(ctx context.Context, delivery progressDelivery) {
	callCtx, cancel := context.WithTimeout(ctx, q.policy.rpcTimeout)
	pagesPerMinute := delivery.report.PagesPerMinute
	_, err := q.client.ReportProgress(callCtx, &crawlrpc.CrawlProgressReport{
		WorkerId:       q.workerID,
		RunId:          delivery.report.Provenance,
		ProfileHandle:  delivery.report.ProfileHandle,
		ProfileName:    delivery.report.ProfileName,
		State:          protoRunState(delivery.report.State),
		Tally:          protoRunTally(delivery.report.Tally),
		PagesPerMinute: &pagesPerMinute,
	})
	cancel()
	q.settle(delivery, err)
}

func (q *progressDeliveryQueue) settle(delivery progressDelivery, deliveryErr error) {
	key := string(delivery.report.Provenance)
	warn := false

	q.mu.Lock()
	sequence, exists := q.pending[key]
	if exists && sequence[0].version == delivery.version {
		current := sequence[0]
		switch {
		case deliveryErr == nil || !current.terminal:
			q.removeHeadLocked(key, sequence)
		case current.terminal:
			if !current.warned {
				warn = true
				current.warned = true
			}
			current.due = time.Now().Add(progressRetryDelay(
				current.retryWait,
				q.policy.entropy,
			))
			current.retryWait = min(q.policy.retryMaximum, current.retryWait*2)
			sequence[0] = current
			q.pending[key] = sequence
		}
		q.signalLocked()
	}
	q.mu.Unlock()

	if deliveryErr != nil && (warn || !delivery.terminal) {
		slog.WarnContext(context.Background(), msgProgressReportFailed,
			slog.String("runId", key),
			slog.String("state", string(delivery.report.State)),
			slog.String("workerId", q.workerID),
			slog.Any("error", deliveryErr))
	}
}

func (q *progressDeliveryQueue) evictExpendableRunningTailLocked() {
	var selectedKey string
	var selectedDelivery progressDelivery
	expendableFound := false
	for key, sequence := range q.pending {
		delivery := sequence[len(sequence)-1]
		if delivery.terminal || len(sequence) > 1 && sequence[len(sequence)-2].terminal {
			continue
		}
		if !expendableFound || delivery.version < selectedDelivery.version {
			selectedKey = key
			selectedDelivery = delivery
			expendableFound = true
		}
	}
	if expendableFound {
		q.queued--
		delete(q.pending, selectedKey)
	}
}

func (q *progressDeliveryQueue) removeHeadLocked(
	key string,
	sequence []progressDelivery,
) {
	q.queued--
	if len(sequence) == 1 {
		delete(q.pending, key)

		return
	}
	q.pending[key] = sequence[1:]
}

func (q *progressDeliveryQueue) signalLocked() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func terminalProgressState(state yagocrawlcontract.CrawlRunState) bool {
	return state == yagocrawlcontract.CrawlRunFinished ||
		state == yagocrawlcontract.CrawlRunCancelled
}

func progressRetryDelay(wait time.Duration, entropy io.Reader) time.Duration {
	half := wait / 2
	offset, err := cryptorand.Int(entropy, big.NewInt(int64(wait-half)))
	if err != nil {
		return half
	}

	return half + time.Duration(offset.Int64())
}
