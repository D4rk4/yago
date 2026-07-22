package peernews

import (
	"container/heap"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type retainedNewsAdmission struct {
	record   retainedNewsRecord
	retained bool
}

type newsRetentionPlan struct {
	records    *boundedNewestNews
	removed    []retainedNewsRecord
	deleteKeys []vault.Key
	pending    []retainedNewsAdmission
}

func prepareNewsRetention(
	records *boundedNewestNews,
	excluded []vault.Key,
	pending []retainedNewsRecord,
) newsRetentionPlan {
	plan := newsRetentionPlan{records: records}
	for _, key := range excluded {
		plan.deleteKeys = append(plan.deleteKeys, append(vault.Key(nil), key...))
		if record, found := records.Remove(key); found {
			plan.removed = append(plan.removed, record)
		}
	}
	pendingKeys := make(map[string]struct{}, len(pending))
	for _, record := range pending {
		pendingKeys[string(record.key)] = struct{}{}
	}
	for _, record := range pending {
		for _, evicted := range records.Add(record) {
			if _, isPending := pendingKeys[string(evicted.key)]; !isPending {
				plan.removed = append(plan.removed, evicted)
				plan.deleteKeys = append(
					plan.deleteKeys, append(vault.Key(nil), evicted.key...),
				)
			}
		}
	}
	for _, record := range pending {
		plan.pending = append(plan.pending, retainedNewsAdmission{
			record: record, retained: records.Contains(record.key),
		})
	}

	return plan
}

func (p newsRetentionPlan) rollback() {
	for _, admission := range p.pending {
		p.records.Remove(admission.record.key)
	}
	for _, record := range p.removed {
		p.records.Add(record)
	}
}

func (p newsRetentionPlan) replacePendingKey(provisional, actual vault.Key) {
	record := p.records.keys[string(provisional)]
	delete(p.records.keys, string(provisional))
	record.key = append(vault.Key(nil), actual...)
	p.records.keys[string(actual)] = record
	heap.Fix(&p.records.records, record.index)
}

func (p newsRetentionPlan) retains(key vault.Key) bool {
	for _, admission := range p.pending {
		if string(admission.record.key) == string(key) {
			return admission.retained
		}
	}

	return false
}
