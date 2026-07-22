package peernews

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func pendingKnownNewsKey(id string) vault.Key {
	return vault.Key("pending-known/" + id)
}

func pendingQueuedNewsKey(queue Queue, id string) vault.Key {
	return vault.Key("pending-queue/" + string(queue) + "/" + id)
}

func queuedRetentionRecord(queue Queue, key vault.Key, record Record) retainedNewsRecord {
	return retainedNewsRecord{
		key: key, tie: vault.Key(record.ID() + "\x00" + string(queue)),
		created: record.Created, bytes: len(record.WireForm()),
	}
}

func (p *Pool) deleteKnownRetentionPlan(tx *vault.Txn, plan newsRetentionPlan) error {
	for _, key := range plan.deleteKeys {
		if err := p.forgetKnownNews(tx, key); err != nil {
			return fmt.Errorf("evict known news: %w", err)
		}
	}

	return nil
}

func (p *Pool) deleteQueueRetentionPlan(tx *vault.Txn, plan newsRetentionPlan) error {
	for _, key := range plan.deleteKeys {
		_, err := p.queue.Delete(tx, key)
		if err != nil {
			return fmt.Errorf("evict queued news: %w", err)
		}
	}

	return nil
}

func (p *Pool) syncStoredState() {
	p.stored = newsStoredState{
		queueRecords: p.queuedNewsRetention.records.Len(),
		queueBytes:   p.queuedNewsRetention.bytes,
		knownRecords: p.knownNewsRetention.records.Len(),
	}
}

func (p *Pool) applyRetentionLimits() {
	p.knownNewsRetention.recordLimit = p.retention.knownRecords
	p.queuedNewsRetention.recordLimit = p.retention.queueRecords
	p.queuedNewsRetention.byteLimit = p.retention.queueBytes
}
