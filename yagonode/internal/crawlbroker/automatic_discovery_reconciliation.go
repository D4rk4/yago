package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type automaticDiscoveryIntent struct {
	key  string
	data []byte
}

func (q *DurableOrderQueue) reconcileAutomaticDiscoveryIntents(ctx context.Context) error {
	intents := make([]automaticDiscoveryIntent, 0, 1)
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.discoveryIntents.Scan(tx, nil, func(
			key vault.Key,
			data []byte,
		) (bool, error) {
			intents = append(intents, automaticDiscoveryIntent{
				key:  string(key),
				data: append([]byte(nil), data...),
			})

			return true, nil
		})
	}); err != nil {
		return fmt.Errorf("read automatic crawl discovery intents: %w", err)
	}
	for _, intent := range intents {
		_, err := q.completeAutomaticDiscoveryIntent(
			ctx,
			intent.key,
			intent.data,
			true,
		)
		if err != nil {
			return err
		}
		q.signal()
		if err := q.releaseAutomaticDiscoveryIntent(ctx, intent.key); err != nil {
			return err
		}
	}

	return nil
}

func (q *DurableOrderQueue) reconcileAutomaticDiscoveryLeases(ctx context.Context) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		return q.leases.Scan(tx, nil, func(
			leaseKey vault.Key,
			record leaseRecord,
		) (bool, error) {
			if record.DiscoveryKey == "" {
				return true, nil
			}
			if err := q.recordLeasedAutomaticDiscoveryTx(
				tx,
				record.DiscoveryKey,
				leaseKey,
			); err != nil {
				return false, err
			}
			_, found, err := q.activeDiscoveryKeys.Get(
				tx,
				vault.Key(record.DiscoveryKey),
			)
			if err != nil {
				return false, fmt.Errorf("read leased crawl discovery: %w", err)
			}
			if found {
				return true, nil
			}
			if err := q.activeDiscoveryKeys.Put(
				tx,
				vault.Key(record.DiscoveryKey),
				record.DiscoverySequence,
			); err != nil {
				return false, fmt.Errorf("repair leased crawl discovery: %w", err)
			}

			return true, nil
		})
	}); err != nil {
		return fmt.Errorf("reconcile leased crawl discoveries: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) persistAutomaticDiscoveryIntent(
	ctx context.Context,
	key string,
	data []byte,
) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		return q.discoveryIntents.Put(tx, vault.Key(key), data)
	}); err != nil {
		return fmt.Errorf("persist automatic crawl discovery intent: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) completeAutomaticDiscoveryIntent(
	ctx context.Context,
	key string,
	data []byte,
	recovering bool,
) (bool, error) {
	duplicate := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		duplicate, err = q.automaticDiscoveryOutstandingTx(tx, key, true)
		if err != nil || duplicate {
			return err
		}
		if recovering {
			duplicate, err = q.untrackedAutomaticDiscoveryTx(tx, key, true)
			if err != nil || duplicate {
				return err
			}
		}
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
		if err != nil {
			return fmt.Errorf("decode automatic crawl discovery intent: %w", err)
		}
		sequence, err := q.enqueueTx(tx, data, order.Priority)
		if err != nil {
			return err
		}
		if err := q.activeDiscoveryKeys.Put(tx, vault.Key(key), sequence); err != nil {
			return fmt.Errorf("record active crawl discovery: %w", err)
		}
		if err := q.pendingDiscoveryKeys.Put(
			tx,
			orderKey(sequence),
			[]byte(key),
		); err != nil {
			return fmt.Errorf("record pending crawl discovery: %w", err)
		}

		return nil
	}); err != nil {
		return false, fmt.Errorf("complete automatic crawl discovery intent: %w", err)
	}

	return duplicate, nil
}

func (q *DurableOrderQueue) releaseAutomaticDiscoveryIntent(
	ctx context.Context,
	key string,
) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		_, err := q.discoveryIntents.Delete(tx, vault.Key(key))
		if err != nil {
			return fmt.Errorf("delete automatic crawl discovery intent: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("release automatic crawl discovery intent: %w", err)
	}

	return nil
}
