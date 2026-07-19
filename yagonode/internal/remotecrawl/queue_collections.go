package remotecrawl

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	remoteCrawlOrderBucket       vault.Name = "remotecrawlorders"
	remoteCrawlURLSequenceBucket vault.Name = "remotecrawlurlsequences"
	remoteCrawlSequenceBucket    vault.Name = "remotecrawlsequence"
	remoteCrawlRequestRateBucket vault.Name = "remotecrawlrequestrates"
	remoteCrawlLeaseCountBucket  vault.Name = "remotecrawlactiveleases"
	remoteCrawlLeaseExpiryBucket vault.Name = "remotecrawlleaseexpiries"
	remoteCrawlPendingBucket     vault.Name = "remotecrawlpending"
	remoteCrawlSchemaBucket      vault.Name = "remotecrawlschema"
)

var (
	nextSequenceKey      = vault.Key("next")
	queueStateVersionKey = vault.Key("queue-state-version")
)

const currentQueueStateVersion uint64 = 2

type queueState string

const (
	queueStatePending queueState = "pending"
	queueStateLeased  queueState = "leased"
)

type queueRecord struct {
	Sequence    uint64     `json:"sequence"`
	URL         string     `json:"url"`
	URLHash     string     `json:"url_hash"`
	Referrer    string     `json:"referrer,omitempty"`
	Description string     `json:"description,omitempty"`
	PublishedAt int64      `json:"published_at"`
	State       queueState `json:"state"`
	Peer        string     `json:"peer,omitempty"`
	LeaseUntil  int64      `json:"lease_until,omitempty"`
	Attempts    uint32     `json:"attempts,omitempty"`
}

type requestRateRecord struct {
	WindowStart int64 `json:"window_start"`
	Requests    int   `json:"requests"`
}

type leaseExpiryRecord struct {
	Sequence uint64 `json:"sequence"`
}

type pendingRecord struct {
	Sequence uint64 `json:"sequence"`
}

type jsonCodec[Value any] struct{}

func (jsonCodec[Value]) Encode(value Value) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode remote crawl record: %w", err)
	}

	return raw, nil
}

func (jsonCodec[Value]) Decode(raw []byte) (Value, error) {
	var value Value
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode remote crawl record: %w", err)
	}

	return value, nil
}

type uint64Codec struct{}

func (uint64Codec) Encode(value uint64) ([]byte, error) {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, value)

	return raw, nil
}

func (uint64Codec) Decode(raw []byte) (uint64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("decode remote crawl sequence: length %d", len(raw))
	}

	return binary.BigEndian.Uint64(raw), nil
}

type collections struct {
	orders        *vault.Collection[queueRecord]
	urlSequences  *vault.Keyspace[uint64]
	sequence      *vault.Keyspace[uint64]
	requestRates  *vault.Keyspace[requestRateRecord]
	leaseCounts   *vault.Collection[uint64]
	leaseExpiries *vault.Collection[leaseExpiryRecord]
	pending       *vault.Collection[pendingRecord]
	schema        *vault.Keyspace[uint64]
}

func registerCollections(storage *vault.Vault) (collections, error) {
	orders, err := vault.Register(storage, remoteCrawlOrderBucket, jsonCodec[queueRecord]{})
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl orders: %w", err)
	}
	urlSequences, err := vault.RegisterKeyspace(
		storage,
		remoteCrawlURLSequenceBucket,
		uint64Codec{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl URL sequences: %w", err)
	}
	sequence, err := vault.RegisterKeyspace(
		storage,
		remoteCrawlSequenceBucket,
		uint64Codec{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl sequence: %w", err)
	}
	requestRates, err := vault.RegisterKeyspace(
		storage,
		remoteCrawlRequestRateBucket,
		jsonCodec[requestRateRecord]{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl request rates: %w", err)
	}
	leaseCounts, err := vault.Register(
		storage,
		remoteCrawlLeaseCountBucket,
		uint64Codec{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl lease counts: %w", err)
	}
	leaseExpiries, err := vault.Register(
		storage,
		remoteCrawlLeaseExpiryBucket,
		jsonCodec[leaseExpiryRecord]{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl lease expiries: %w", err)
	}
	pending, err := vault.Register(
		storage,
		remoteCrawlPendingBucket,
		jsonCodec[pendingRecord]{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl pending orders: %w", err)
	}
	schema, err := vault.RegisterKeyspace(
		storage,
		remoteCrawlSchemaBucket,
		uint64Codec{},
	)
	if err != nil {
		return collections{}, fmt.Errorf("register remote crawl schema: %w", err)
	}

	return collections{
		orders: orders, urlSequences: urlSequences, sequence: sequence,
		requestRates: requestRates, leaseCounts: leaseCounts,
		leaseExpiries: leaseExpiries,
		pending:       pending,
		schema:        schema,
	}, nil
}

func sequenceKey(sequence uint64) vault.Key {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, sequence)

	return vault.Key(raw)
}

func leaseExpiryKey(expiry int64, sequence uint64) vault.Key {
	raw := make([]byte, 16)
	_, _ = binary.Encode(raw[:8], binary.BigEndian, expiry)
	binary.BigEndian.PutUint64(raw[8:], sequence)

	return vault.Key(raw)
}

func leaseExpiry(key vault.Key) (int64, bool) {
	if len(key) != 16 {
		return 0, false
	}
	var expiry int64
	_, err := binary.Decode(key[:8], binary.BigEndian, &expiry)

	return expiry, err == nil
}
