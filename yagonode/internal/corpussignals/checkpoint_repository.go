package corpussignals

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	checkpointFormat                                = "yago-corpus-signals-v1"
	maximumCheckpointBytes                          = 24 << 20
	maximumCheckpointCitations                      = 4_096
	maximumCheckpointAuthorityDomains               = 8_192
	maximumCheckpointSpellingTerms                  = 8_192
	maximumCheckpointWordFormTerms                  = 32_768
	maximumCheckpointTrustDomains                   = 256
	maximumCheckpointDomainBytes                    = 253
	maximumCheckpointURLBytes                       = 2 << 10
	maximumCheckpointVocabularyTermBytes            = 128
	checkpointBucket                     vault.Name = "corpus_signal_checkpoints"
)

var checkpointKey = vault.Key("current")

type Checkpoint struct {
	Authority            hostrank.AuthorityTable `json:"authority"`
	Citations            []hostrank.Citation     `json:"citations"`
	Spelling             map[string]int          `json:"spelling"`
	WordForms            map[string]int          `json:"word_forms"`
	WordFormsReady       bool                    `json:"word_forms_ready"`
	HostLinks            hostlinkgraph.Graph     `json:"host_links"`
	HostLinksReady       bool                    `json:"host_links_ready"`
	TrustDomains         []string                `json:"trust_domains"`
	TrustBlend           float64                 `json:"trust_blend"`
	CompletedAtUnixMilli int64                   `json:"completed_at_unix_milli"`
}

type checkpointRecord struct {
	Format     string     `json:"format"`
	Checkpoint Checkpoint `json:"checkpoint"`
}

type checkpointCodec struct{}

func (checkpointCodec) Encode(record checkpointRecord) ([]byte, error) {
	if err := validateCheckpointRecord(record); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(record)
	if len(encoded) > maximumCheckpointBytes {
		return nil, fmt.Errorf("corpus signal checkpoint exceeds %d bytes", maximumCheckpointBytes)
	}

	return encoded, nil
}

func (checkpointCodec) Decode(encoded []byte) (checkpointRecord, error) {
	if len(encoded) == 0 || len(encoded) > maximumCheckpointBytes {
		return checkpointRecord{}, fmt.Errorf(
			"invalid corpus signal checkpoint size: %d",
			len(encoded),
		)
	}
	var record checkpointRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return checkpointRecord{}, fmt.Errorf("decode corpus signal checkpoint: %w", err)
	}
	if err := validateCheckpointRecord(record); err != nil {
		return checkpointRecord{}, err
	}
	record.Checkpoint = cloneCheckpoint(record.Checkpoint)

	return record, nil
}

type CheckpointRepository struct {
	storage *vault.Vault
	records *vault.Collection[checkpointRecord]
}

func Open(storage *vault.Vault) (*CheckpointRepository, error) {
	records, err := vault.Register(storage, checkpointBucket, checkpointCodec{})
	if err != nil {
		return nil, fmt.Errorf("register corpus signal checkpoints: %w", err)
	}

	return &CheckpointRepository{storage: storage, records: records}, nil
}

func (r *CheckpointRepository) Load(ctx context.Context) (Checkpoint, bool, error) {
	var checkpoint Checkpoint
	found := false
	if err := r.storage.View(ctx, func(tx *vault.Txn) error {
		record, exists, err := r.records.Get(tx, checkpointKey)
		if err != nil {
			return fmt.Errorf("read corpus signal checkpoint: %w", err)
		}
		if exists {
			checkpoint = record.Checkpoint
			found = true
		}

		return nil
	}); err != nil {
		return Checkpoint{}, false, fmt.Errorf("load corpus signal checkpoint: %w", err)
	}

	return cloneCheckpoint(checkpoint), found, nil
}

func (r *CheckpointRepository) Replace(ctx context.Context, checkpoint Checkpoint) error {
	record := checkpointRecord{Format: checkpointFormat, Checkpoint: checkpoint}
	if err := validateCheckpointRecord(record); err != nil {
		return err
	}
	record.Checkpoint = cloneCheckpoint(checkpoint)
	if err := r.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := r.records.Put(tx, checkpointKey, record); err != nil {
			return fmt.Errorf("write corpus signal checkpoint: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist corpus signal checkpoint: %w", err)
	}

	return nil
}

func validateCheckpointRecord(record checkpointRecord) error {
	if record.Format != checkpointFormat {
		return fmt.Errorf("unsupported corpus signal checkpoint format %q", record.Format)
	}
	checkpoint := record.Checkpoint
	if err := validateCheckpointCollections(checkpoint); err != nil {
		return err
	}
	if checkpoint.CompletedAtUnixMilli <= 0 {
		return fmt.Errorf("corpus signal checkpoint completion time is invalid")
	}
	if math.IsNaN(checkpoint.TrustBlend) || math.IsInf(checkpoint.TrustBlend, 0) ||
		checkpoint.TrustBlend < 0 || checkpoint.TrustBlend > 1 {
		return fmt.Errorf("corpus signal checkpoint trust blend is invalid")
	}
	if err := validateAuthority(checkpoint.Authority); err != nil {
		return err
	}
	if err := validateCitations(checkpoint.Citations); err != nil {
		return err
	}
	if err := validateVocabulary(checkpoint.Spelling); err != nil {
		return fmt.Errorf("invalid spelling checkpoint: %w", err)
	}
	if err := validateVocabulary(checkpoint.WordForms); err != nil {
		return fmt.Errorf("invalid word-form checkpoint: %w", err)
	}
	seenTrustDomains := make(map[string]struct{}, len(checkpoint.TrustDomains))
	for _, domain := range checkpoint.TrustDomains {
		if !validCheckpointDomain(domain) {
			return fmt.Errorf("invalid checkpoint trust domain %q", domain)
		}
		if _, exists := seenTrustDomains[domain]; exists {
			return fmt.Errorf("duplicate checkpoint trust domain %q", domain)
		}
		seenTrustDomains[domain] = struct{}{}
	}

	return nil
}

func validateAuthority(authority hostrank.AuthorityTable) error {
	for domain, evidence := range authority {
		if !validCheckpointDomain(domain) || math.IsNaN(evidence.Score) ||
			math.IsInf(evidence.Score, 0) || evidence.Score < 0 || evidence.Score > 1 ||
			math.IsNaN(evidence.Confidence) || math.IsInf(evidence.Confidence, 0) ||
			evidence.Confidence < 0 || evidence.Confidence > 1 {
			return fmt.Errorf("invalid checkpoint authority for %q", domain)
		}
	}

	return nil
}

func validateCitations(citations []hostrank.Citation) error {
	for _, citation := range citations {
		if len(citation.SourceURL) > maximumCheckpointURLBytes ||
			len(citation.TargetURL) > maximumCheckpointURLBytes {
			return fmt.Errorf("checkpoint citation URL exceeds limit")
		}
	}
	sample := hostrank.NewCitationSample()
	sample.Add(citations...)
	if len(sample.Citations()) != len(citations) {
		return fmt.Errorf("checkpoint citations contain invalid or duplicate evidence")
	}

	return nil
}

func validateVocabulary(vocabulary map[string]int) error {
	for term, frequency := range vocabulary {
		if term == "" || term != strings.TrimSpace(term) ||
			len(term) > maximumCheckpointVocabularyTermBytes || frequency <= 0 {
			return fmt.Errorf("invalid checkpoint vocabulary term %q", term)
		}
	}

	return nil
}

func validCheckpointDomain(domain string) bool {
	return domain != "" && domain == strings.TrimSpace(domain) &&
		domain == strings.ToLower(domain) && len(domain) <= maximumCheckpointDomainBytes
}

func cloneCheckpoint(checkpoint Checkpoint) Checkpoint {
	authority := make(hostrank.AuthorityTable, len(checkpoint.Authority))
	for domain, evidence := range checkpoint.Authority {
		authority[strings.Clone(domain)] = evidence
	}
	citations := make([]hostrank.Citation, len(checkpoint.Citations))
	for index, citation := range checkpoint.Citations {
		citations[index] = hostrank.Citation{
			SourceURL:  strings.Clone(citation.SourceURL),
			TargetURL:  strings.Clone(citation.TargetURL),
			Confidence: citation.Confidence,
		}
	}
	spelling := cloneVocabulary(checkpoint.Spelling)
	wordForms := cloneVocabulary(checkpoint.WordForms)
	hostLinks := hostlinkgraph.Clone(checkpoint.HostLinks)
	trustDomains := make([]string, len(checkpoint.TrustDomains))
	for index, domain := range checkpoint.TrustDomains {
		trustDomains[index] = strings.Clone(domain)
	}

	return Checkpoint{
		Authority: authority, Citations: citations, Spelling: spelling, WordForms: wordForms,
		WordFormsReady: checkpoint.WordFormsReady, HostLinks: hostLinks,
		HostLinksReady: checkpoint.HostLinksReady, TrustDomains: trustDomains,
		TrustBlend: checkpoint.TrustBlend, CompletedAtUnixMilli: checkpoint.CompletedAtUnixMilli,
	}
}

func cloneVocabulary(vocabulary map[string]int) map[string]int {
	cloned := make(map[string]int, len(vocabulary))
	for term, frequency := range vocabulary {
		cloned[strings.Clone(term)] = frequency
	}

	return cloned
}
