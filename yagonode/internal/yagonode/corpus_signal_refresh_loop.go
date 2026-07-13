package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

const (
	defaultCorpusSignalRefreshInterval = 10 * time.Minute
	corpusSignalRefreshFailedMessage   = "corpus signal refresh scan failed"
	hostAuthorityRefreshFailedMessage  = "host authority refresh failed"
	spellMinTermFrequency              = 2
	spellVocabularyTerms               = 8_192
	wordFormsVocabularyTerms           = 32_768
)

type hostTrustPolicySource interface {
	Current() hosttrust.Policy
	Changes() <-chan struct{}
}

type corpusSignalRefresh struct {
	documents        documentstore.StoredDocuments
	hostRank         *hostrank.Holder
	spell            *spellcheck.Holder
	wordForms        *wordforms.Holder
	includeWordForms bool
	trust            hostTrustPolicySource
	citations        []hostrank.Citation
	citationsReady   bool
}

var newCorpusSignalRefreshDelay = func(interval time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(interval)

	return timer.C, func() { timer.Stop() }
}

func runCorpusSignalRefreshLoop(ctx context.Context, refresh *corpusSignalRefresh) {
	for ctx.Err() == nil {
		refresh.scanAndPublish(ctx)
		if !refresh.wait(ctx) {
			return
		}
	}
}

func (r *corpusSignalRefresh) wait(ctx context.Context) bool {
	delay, stop := newCorpusSignalRefreshDelay(defaultCorpusSignalRefreshInterval)
	defer stop()
	var trustChanges <-chan struct{}
	if r.trust != nil {
		trustChanges = r.trust.Changes()
	}
	for {
		select {
		case <-ctx.Done():
			return false
		case <-delay:
			return true
		case _, open := <-trustChanges:
			if !open {
				trustChanges = nil
				continue
			}
			r.publishAuthority(ctx)
		}
	}
}

func (r *corpusSignalRefresh) scanAndPublish(ctx context.Context) {
	if r.documents == nil {
		return
	}

	citationSample := hostrank.NewCitationSample()
	spellFrequency := spellcheck.NewFrequencySynopsis(spellVocabularyTerms)
	var wordFormsFrequency *spellcheck.FrequencySynopsis
	if r.includeWordForms {
		wordFormsFrequency = spellcheck.NewFrequencySynopsis(wordFormsVocabularyTerms)
	}
	err := r.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		visitDocumentAuthorityCitations(doc, func(citation hostrank.Citation) {
			citationSample.Add(citation)
		})
		spellFrequency.ObserveText(doc.Title)
		spellFrequency.ObserveText(doc.ExtractedText)
		if wordFormsFrequency != nil {
			wordFormsFrequency.ObserveText(doc.Title)
			wordFormsFrequency.ObserveText(doc.ExtractedText)
		}

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, corpusSignalRefreshFailedMessage, slog.Any("error", err))

		return
	}

	citations := citationSample.Citations()
	var authority hostrank.AuthorityTable
	if r.hostRank != nil {
		table, err := r.computeAuthority(ctx, citations)
		if err != nil {
			slog.WarnContext(ctx, hostAuthorityRefreshFailedMessage, slog.Any("error", err))

			return
		}
		authority = table
	}
	var corrector *spellcheck.Corrector
	if r.spell != nil {
		corrector = spellcheck.New(prunedVocabulary(spellFrequency.Frequencies()))
	}
	var expander *wordforms.Expander
	if r.wordForms != nil && wordFormsFrequency != nil {
		expander = wordforms.New(wordFormsFrequency.Frequencies(), searchindex.StemWord)
	}

	r.citations = citations
	r.citationsReady = true
	if r.hostRank != nil {
		r.hostRank.Store(authority)
	}
	if r.spell != nil {
		r.spell.Store(corrector)
	}
	if expander != nil {
		r.wordForms.Store(expander)
	}
}

func (r *corpusSignalRefresh) publishAuthority(ctx context.Context) {
	if r.hostRank == nil || !r.citationsReady {
		return
	}
	table, err := r.computeAuthority(ctx, r.citations)
	if err != nil {
		slog.WarnContext(ctx, hostAuthorityRefreshFailedMessage, slog.Any("error", err))

		return
	}
	r.hostRank.Store(table)
}

func (r *corpusSignalRefresh) computeAuthority(
	ctx context.Context,
	citations []hostrank.Citation,
) (hostrank.AuthorityTable, error) {
	options := hostrank.DomainOptions{}
	if r.trust != nil {
		policy := r.trust.Current()
		options.TrustedDomains = policy.Domains
		options.TrustBlend = policy.Blend
	}

	table, err := hostrank.ComputeDomainAuthority(ctx, citations, options)
	if err != nil {
		return nil, fmt.Errorf("compute host authority: %w", err)
	}

	return table, nil
}

func prunedVocabulary(frequency map[string]int) map[string]int {
	pruned := make(map[string]int, len(frequency))
	for term, count := range frequency {
		if count >= spellMinTermFrequency {
			pruned[term] = count
		}
	}

	return pruned
}
