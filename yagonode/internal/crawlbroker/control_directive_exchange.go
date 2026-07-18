package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (l *persistentControlDirectiveLedger) workerDirectives(
	ctx context.Context,
	workerID string,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	var directives []yagocrawlcontract.CrawlControlDirective
	err := l.storage.View(ctx, func(tx *vault.Txn) error {
		var err error
		directives, err = l.workerDirectivesTx(tx, workerID)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read crawl control directives: %w", err)
	}

	return directives, nil
}

func (l *persistentControlDirectiveLedger) workerDirectivesTx(
	tx *vault.Txn,
	workerID string,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	directives := make([]yagocrawlcontract.CrawlControlDirective, 0)
	err := l.directives.Scan(tx, nil, func(
		_ vault.Key,
		record controlDirectiveRecord,
	) (bool, error) {
		if record.WorkerID != workerID {
			return true, nil
		}
		directives = append(directives, record.Directive)

		return len(directives) < yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan crawl control directives: %w", err)
	}

	return directives, nil
}

func (l *persistentControlDirectiveLedger) acknowledgeWorkerDirectivesTx(
	tx *vault.Txn,
	workerID string,
	acknowledged []uint64,
) error {
	for _, directiveID := range acknowledged {
		key := orderKey(directiveID)
		record, found, err := l.directives.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read acknowledged crawl control directive: %w", err)
		}
		if !found || record.WorkerID != workerID {
			continue
		}
		if _, err := l.directives.Delete(tx, key); err != nil {
			return fmt.Errorf("delete acknowledged crawl control directive: %w", err)
		}
	}

	return nil
}
