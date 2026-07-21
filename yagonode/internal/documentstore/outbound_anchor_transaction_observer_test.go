package documentstore

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type outboundAnchorTransactionObservation struct {
	buckets      map[vault.Name]struct{}
	encodedBytes int
	err          error
	puts         map[vault.Name]int
	rows         int
	targets      []string
	targetSet    map[string]struct{}
}

type outboundAnchorObservingEngine struct {
	base         *pagedDocumentEngine
	mutex        sync.Mutex
	afterUpdate  func(outboundAnchorTransactionObservation)
	failBucket   vault.Name
	failError    error
	failTarget   string
	failed       bool
	gets         map[vault.Name]map[string]int
	transactions []outboundAnchorTransactionObservation
}

func newOutboundAnchorObservingEngine() *outboundAnchorObservingEngine {
	return &outboundAnchorObservingEngine{
		base: newPagedDocumentEngine(),
		gets: make(map[vault.Name]map[string]int),
	}
}

func (e *outboundAnchorObservingEngine) Provision(name vault.Name) error {
	return e.base.Provision(name)
}

func (e *outboundAnchorObservingEngine) Update(
	ctx context.Context,
	visit func(vault.EngineTxn) error,
) error {
	observation := outboundAnchorTransactionObservation{
		buckets:   make(map[vault.Name]struct{}),
		puts:      make(map[vault.Name]int),
		targetSet: make(map[string]struct{}),
	}
	err := e.base.Update(ctx, func(tx vault.EngineTxn) error {
		return visit(outboundAnchorObservingTransaction{
			EngineTxn:   tx,
			engine:      e,
			observation: &observation,
		})
	})
	observation.err = err
	e.mutex.Lock()
	e.transactions = append(e.transactions, observation)
	afterUpdate := e.afterUpdate
	e.mutex.Unlock()
	if afterUpdate != nil {
		afterUpdate(observation)
	}

	return err
}

func (e *outboundAnchorObservingEngine) View(
	ctx context.Context,
	visit func(vault.EngineTxn) error,
) error {
	return e.base.View(ctx, func(tx vault.EngineTxn) error {
		return visit(outboundAnchorObservingTransaction{
			EngineTxn: tx,
			engine:    e,
		})
	})
}

func (e *outboundAnchorObservingEngine) UsedBytes(ctx context.Context) (int64, error) {
	return e.base.UsedBytes(ctx)
}

func (e *outboundAnchorObservingEngine) QuotaBytes() int64 {
	return e.base.QuotaBytes()
}

func (e *outboundAnchorObservingEngine) Close() error {
	return e.base.Close()
}

func (e *outboundAnchorObservingEngine) observations() []outboundAnchorTransactionObservation {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return append([]outboundAnchorTransactionObservation(nil), e.transactions...)
}

func (e *outboundAnchorObservingEngine) clearObservations() {
	e.mutex.Lock()
	e.transactions = nil
	e.gets = make(map[vault.Name]map[string]int)
	e.mutex.Unlock()
}

func (e *outboundAnchorObservingEngine) observeAfterUpdate(
	observe func(outboundAnchorTransactionObservation),
) {
	e.mutex.Lock()
	e.afterUpdate = observe
	e.mutex.Unlock()
}

func (e *outboundAnchorObservingEngine) failOnce(
	target string,
	err error,
) {
	e.failOnceIn(inboundAnchorBucket, target, err)
}

func (e *outboundAnchorObservingEngine) failPublicationOnce(
	source string,
	err error,
) {
	e.failOnceIn(outboundAnchorPublicationBucket, source, err)
}

func (e *outboundAnchorObservingEngine) failOnceIn(
	bucket vault.Name,
	target string,
	err error,
) {
	e.mutex.Lock()
	e.failBucket = bucket
	e.failTarget = target
	e.failError = err
	e.failed = false
	e.mutex.Unlock()
}

func (e *outboundAnchorObservingEngine) clearFailure() {
	e.mutex.Lock()
	e.failBucket = ""
	e.failTarget = ""
	e.failError = nil
	e.failed = false
	e.mutex.Unlock()
}

func (e *outboundAnchorObservingEngine) fail(
	bucket vault.Name,
	key vault.Key,
) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.failed || e.failError == nil || bucket != e.failBucket ||
		string(key) != e.failTarget {
		return nil
	}
	e.failed = true

	return e.failError
}

func (e *outboundAnchorObservingEngine) recordGet(
	bucket vault.Name,
	key vault.Key,
) {
	e.mutex.Lock()
	if e.gets[bucket] == nil {
		e.gets[bucket] = make(map[string]int)
	}
	e.gets[bucket][string(key)]++
	e.mutex.Unlock()
}

func (e *outboundAnchorObservingEngine) getTotal(
	bucket vault.Name,
	key string,
) int {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.gets[bucket][key]
}

type outboundAnchorObservingTransaction struct {
	vault.EngineTxn
	engine      *outboundAnchorObservingEngine
	observation *outboundAnchorTransactionObservation
}

func (t outboundAnchorObservingTransaction) Bucket(
	name vault.Name,
) vault.EngineBucket {
	return outboundAnchorObservingBucket{
		EngineBucket: t.EngineTxn.Bucket(name),
		engine:       t.engine,
		name:         name,
		observation:  t.observation,
	}
}

type outboundAnchorObservingBucket struct {
	vault.EngineBucket
	engine      *outboundAnchorObservingEngine
	name        vault.Name
	observation *outboundAnchorTransactionObservation
}

func (b outboundAnchorObservingBucket) Get(key vault.Key) []byte {
	b.engine.recordGet(b.name, key)

	return b.EngineBucket.Get(key)
}

func (b outboundAnchorObservingBucket) Put(key vault.Key, value []byte) error {
	if err := b.engine.fail(b.name, key); err != nil {
		return err
	}
	b.recordMutation(key, len(key)+len(value))
	if b.observation != nil {
		b.observation.puts[b.name]++
	}

	if err := b.EngineBucket.Put(key, value); err != nil {
		return fmt.Errorf("observe bucket put: %w", err)
	}

	return nil
}

func (b outboundAnchorObservingBucket) Delete(key vault.Key) error {
	if err := b.engine.fail(b.name, key); err != nil {
		return err
	}
	b.recordMutation(key, len(key))

	if err := b.EngineBucket.Delete(key); err != nil {
		return fmt.Errorf("observe bucket delete: %w", err)
	}

	return nil
}

func (b outboundAnchorObservingBucket) Contains(key vault.Key) bool {
	if presence, ok := b.EngineBucket.(interface{ Contains(vault.Key) bool }); ok {
		return presence.Contains(key)
	}

	return b.Get(key) != nil
}

func (b outboundAnchorObservingBucket) ReadPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketPage, error) {
	reader, ok := b.EngineBucket.(interface {
		ReadPageAfter(vault.Key, int) (vault.BucketPage, error)
	})
	if !ok {
		return vault.BucketPage{}, errors.New("bucket page unavailable")
	}

	page, err := reader.ReadPageAfter(after, limit)
	if err != nil {
		return vault.BucketPage{}, fmt.Errorf("observe bucket page: %w", err)
	}

	return page, nil
}

func (b outboundAnchorObservingBucket) LastKey() (vault.Key, error) {
	reader, ok := b.EngineBucket.(interface {
		LastKey() (vault.Key, error)
	})
	if !ok {
		return nil, errors.New("bucket last key unavailable")
	}

	key, err := reader.LastKey()
	if err != nil {
		return nil, fmt.Errorf("observe bucket last key: %w", err)
	}

	return key, nil
}

func (b outboundAnchorObservingBucket) recordMutation(key vault.Key, encodedBytes int) {
	if b.observation == nil {
		return
	}
	b.observation.buckets[b.name] = struct{}{}
	b.observation.encodedBytes += encodedBytes
	b.observation.rows++
	targetURL, ok := outboundAnchorMutationTargetURL(b.name, key)
	if !ok {
		return
	}
	if _, found := b.observation.targetSet[targetURL]; found {
		return
	}
	b.observation.targetSet[targetURL] = struct{}{}
	b.observation.targets = append(b.observation.targets, targetURL)
}

func outboundAnchorMutationTargetURL(
	bucket vault.Name,
	key vault.Key,
) (string, bool) {
	switch bucket {
	case inboundAnchorBucket, bucketName:
		return string(key), true
	case orderedDocumentBucketName:
		_, targetURL, err := decodeOrderedDocumentKey(key)

		return targetURL, err == nil
	default:
		return "", false
	}
}

func outboundAnchorMutationObservations(
	observations []outboundAnchorTransactionObservation,
) []outboundAnchorTransactionObservation {
	mutations := make([]outboundAnchorTransactionObservation, 0, len(observations))
	for _, observation := range observations {
		if len(observation.targets) > 0 ||
			observation.err != nil && !errors.Is(observation.err, context.Canceled) {
			if _, publication := observation.buckets[outboundAnchorPublicationBucket]; !publication {
				mutations = append(mutations, observation)
			}
		}
	}

	return mutations
}
