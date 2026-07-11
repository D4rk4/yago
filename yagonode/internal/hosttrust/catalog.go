package hosttrust

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	MaximumDomains                = 256
	maximumDomainBytes            = 253
	catalogFormat                 = "yago-host-trust-v1"
	catalogBucket      vault.Name = "host_trust"
)

var catalogKey = vault.Key("active")

type Policy struct {
	Blend   float64  `json:"blend"`
	Domains []string `json:"domains"`
}

type catalogRecord struct {
	Format string `json:"format"`
	Policy Policy `json:"policy"`
}

type policyCodec struct{}

func (policyCodec) Encode(record catalogRecord) ([]byte, error) {
	if err := validateCatalogRecord(record); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(record)

	return encoded, nil
}

func (policyCodec) Decode(encoded []byte) (catalogRecord, error) {
	var record catalogRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return catalogRecord{}, fmt.Errorf("decode host trust catalog: %w", err)
	}
	if err := validateCatalogRecord(record); err != nil {
		return catalogRecord{}, err
	}
	record.Policy = clonePolicy(record.Policy)

	return record, nil
}

type Catalog struct {
	storage *vault.Vault
	records *vault.Collection[catalogRecord]
	current atomic.Pointer[Policy]
	changes chan struct{}
}

func Open(ctx context.Context, storage *vault.Vault) (*Catalog, error) {
	records, err := vault.Register(storage, catalogBucket, policyCodec{})
	if err != nil {
		return nil, fmt.Errorf("register host trust catalog: %w", err)
	}
	policy := Policy{Domains: []string{}}
	if err := storage.View(ctx, func(tx *vault.Txn) error {
		record, found, readErr := records.Get(tx, catalogKey)
		if readErr != nil {
			return fmt.Errorf("read host trust catalog: %w", readErr)
		}
		if found {
			policy = record.Policy
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load host trust catalog: %w", err)
	}
	catalog := &Catalog{
		storage: storage,
		records: records,
		changes: make(chan struct{}, 1),
	}
	policy = clonePolicy(policy)
	catalog.current.Store(&policy)

	return catalog, nil
}

func (c *Catalog) Current() Policy {
	if c == nil {
		return Policy{Domains: []string{}}
	}
	policy := c.current.Load()
	if policy == nil {
		return Policy{Domains: []string{}}
	}

	return clonePolicy(*policy)
}

func (c *Catalog) Changes() <-chan struct{} {
	if c == nil {
		return nil
	}

	return c.changes
}

func (c *Catalog) Replace(ctx context.Context, policy Policy) error {
	canonical, err := canonicalPolicy(policy)
	if err != nil {
		return err
	}
	record := catalogRecord{Format: catalogFormat, Policy: canonical}
	if err := c.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := c.records.Put(tx, catalogKey, record); err != nil {
			return fmt.Errorf("write host trust catalog: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist host trust catalog: %w", err)
	}
	stored := clonePolicy(canonical)
	c.current.Store(&stored)
	select {
	case c.changes <- struct{}{}:
	default:
	}

	return nil
}

func canonicalPolicy(policy Policy) (Policy, error) {
	if math.IsNaN(policy.Blend) || math.IsInf(policy.Blend, 0) ||
		policy.Blend < 0 || policy.Blend > 1 {
		return Policy{}, fmt.Errorf("host trust blend must be within zero and one")
	}
	if len(policy.Domains) > MaximumDomains {
		return Policy{}, fmt.Errorf("host trust domains exceed %d", MaximumDomains)
	}
	domains := make([]string, len(policy.Domains))
	for index, value := range policy.Domains {
		domain, err := canonicalDomain(value)
		if err != nil {
			return Policy{}, err
		}
		domains[index] = domain
	}
	slices.Sort(domains)
	domains = slices.Compact(domains)

	return Policy{Blend: policy.Blend, Domains: domains}, nil
}

func canonicalDomain(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("host trust domain must not be empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	domain := hostrank.RegistrableDomain(trimmed)
	if !validDomain(domain) {
		return "", fmt.Errorf("host trust domain %q is invalid", value)
	}

	return domain, nil
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > maximumDomainBytes {
		return false
	}
	if net.ParseIP(domain) != nil {
		return true
	}
	for _, label := range strings.Split(domain, ".") {
		if !validDomainLabel(label) {
			return false
		}
	}

	return true
}

func validDomainLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range []byte(label) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '-' {
			continue
		}

		return false
	}

	return true
}

func validateCatalogRecord(record catalogRecord) error {
	if record.Format != catalogFormat {
		return fmt.Errorf("host trust catalog format is unsupported")
	}
	canonical, err := canonicalPolicy(record.Policy)
	if err != nil {
		return err
	}
	if canonical.Blend != record.Policy.Blend ||
		!slices.Equal(canonical.Domains, record.Policy.Domains) {
		return fmt.Errorf("host trust catalog policy is not canonical")
	}

	return nil
}

func clonePolicy(policy Policy) Policy {
	domains := append([]string(nil), policy.Domains...)
	if domains == nil {
		domains = []string{}
	}

	return Policy{Blend: policy.Blend, Domains: domains}
}
