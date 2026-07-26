package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

const msgCrawlProfileRegistrationFailed = "crawl profile registration failed"

const crawlProfileDispatchRegistryCapacity = 256

type crawlProfileWriter interface {
	RecordProfile(
		context.Context,
		yagocrawlcontract.CrawlProfile,
		yagocrawlcontract.CrawlOrderPriority,
	) error
}

// dispatchedProfile is the cached identity of one registration. The priority is
// part of it because a handle re-dispatched under a different priority must be
// rewritten: the recorded priority is what later bounds a recrawl's page budget.
type dispatchedProfile struct {
	profile  yagocrawlcontract.CrawlProfile
	priority yagocrawlcontract.CrawlOrderPriority
}

type crawlProfileDispatchRegistry struct {
	writer         crawlProfileWriter
	mu             sync.Mutex
	recorded       map[string]dispatchedProfile
	insertionOrder []string
	nextEviction   int
}

func newCrawlProfileDispatchRegistry(
	writer crawlProfileWriter,
) *crawlProfileDispatchRegistry {
	return &crawlProfileDispatchRegistry{
		writer:         writer,
		recorded:       make(map[string]dispatchedProfile),
		insertionOrder: make([]string, 0, crawlProfileDispatchRegistryCapacity),
	}
}

func (r *crawlProfileDispatchRegistry) record(
	ctx context.Context,
	profile yagocrawlcontract.CrawlProfile,
	priority yagocrawlcontract.CrawlOrderPriority,
) {
	dispatched := dispatchedProfile{profile: profile, priority: priority}
	r.mu.Lock()
	defer r.mu.Unlock()
	if recorded, found := r.recorded[profile.Handle]; found &&
		reflect.DeepEqual(recorded, dispatched) {
		return
	}
	if err := r.writer.RecordProfile(ctx, profile, priority); err != nil {
		slog.WarnContext(
			ctx,
			msgCrawlProfileRegistrationFailed,
			slog.String("profile", profile.Handle),
			slog.Any("error", err),
		)

		return
	}
	if _, found := r.recorded[profile.Handle]; !found {
		r.recordHandle(profile.Handle)
	}
	r.recorded[profile.Handle] = dispatched
}

func (r *crawlProfileDispatchRegistry) recordHandle(handle string) {
	if len(r.insertionOrder) < crawlProfileDispatchRegistryCapacity {
		r.insertionOrder = append(r.insertionOrder, handle)

		return
	}
	delete(r.recorded, r.insertionOrder[r.nextEviction])
	r.insertionOrder[r.nextEviction] = handle
	r.nextEviction = (r.nextEviction + 1) % crawlProfileDispatchRegistryCapacity
}

type crawlProfileRegisteringQueue struct {
	inner    crawldispatch.CrawlOrderQueue
	registry *crawlProfileDispatchRegistry
}

func (q crawlProfileRegisteringQueue) PublishOnce(
	ctx context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.registry.record(ctx, order.Profile, order.Priority)
	duplicate, err := q.inner.PublishOnce(ctx, key, order)
	if err != nil {
		return duplicate, fmt.Errorf("publish registered crawl order: %w", err)
	}

	return duplicate, nil
}
