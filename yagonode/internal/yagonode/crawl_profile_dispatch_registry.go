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
	RecordProfile(context.Context, yagocrawlcontract.CrawlProfile) error
}

type crawlProfileDispatchRegistry struct {
	writer         crawlProfileWriter
	mu             sync.Mutex
	recorded       map[string]yagocrawlcontract.CrawlProfile
	insertionOrder []string
	nextEviction   int
}

func newCrawlProfileDispatchRegistry(
	writer crawlProfileWriter,
) *crawlProfileDispatchRegistry {
	return &crawlProfileDispatchRegistry{
		writer:         writer,
		recorded:       make(map[string]yagocrawlcontract.CrawlProfile),
		insertionOrder: make([]string, 0, crawlProfileDispatchRegistryCapacity),
	}
}

func (r *crawlProfileDispatchRegistry) record(
	ctx context.Context,
	profile yagocrawlcontract.CrawlProfile,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if recorded, found := r.recorded[profile.Handle]; found &&
		reflect.DeepEqual(recorded, profile) {
		return
	}
	if err := r.writer.RecordProfile(ctx, profile); err != nil {
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
	r.recorded[profile.Handle] = profile
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
	q.registry.record(ctx, order.Profile)
	duplicate, err := q.inner.PublishOnce(ctx, key, order)
	if err != nil {
		return duplicate, fmt.Errorf("publish registered crawl order: %w", err)
	}

	return duplicate, nil
}
