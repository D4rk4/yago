package crawlbroker

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	leaseControlTargetBucket          vault.Name = "crawlordercontroltargets"
	completedLeaseControlTargetBucket vault.Name = "completedcrawlordercontroltargets"
)

type leaseControlTarget struct {
	WorkerID string `json:"worker"`
	RunID    string `json:"run"`
}

type leaseControlTargetCodec struct{}

func (leaseControlTargetCodec) Encode(target leaseControlTarget) ([]byte, error) {
	raw, _ := json.Marshal(target)

	return raw, nil
}

func (leaseControlTargetCodec) Decode(raw []byte) (leaseControlTarget, error) {
	var target leaseControlTarget
	if err := json.Unmarshal(raw, &target); err != nil {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease control target: %w", err)
	}
	if target.WorkerID == "" || target.RunID == "" {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease control target: empty identity")
	}

	return target, nil
}

func controlTargetFromLease(record leaseRecord) (leaseControlTarget, error) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(record.OrderData)
	if err != nil {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease order: %w", err)
	}

	return leaseControlTarget{
		WorkerID: record.WorkerID,
		RunID:    hex.EncodeToString(order.Provenance),
	}, nil
}
