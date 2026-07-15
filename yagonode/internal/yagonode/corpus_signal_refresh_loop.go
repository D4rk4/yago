package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/corpussignals"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
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
	documents            documentstore.StoredDocuments
	hostRank             *hostrank.Holder
	spell                *spellcheck.Holder
	wordForms            *wordforms.Holder
	hostLinks            *hostlinks.SnapshotHolder
	includeWordForms     bool
	trust                hostTrustPolicySource
	citations            []hostrank.Citation
	citationsReady       bool
	spellingVocabulary   map[string]int
	wordFormsVocabulary  map[string]int
	wordFormsReady       bool
	hostLinkGraph        hostlinks.Graph
	hostLinksReady       bool
	completedAtUnixMilli int64
	checkpoints          corpusSignalCheckpointRepository
	initialRefreshDelay  time.Duration
	readTime             func() time.Time
}

var newCorpusSignalRefreshDelay = func(interval time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(interval)

	return timer.C, func() { timer.Stop() }
}

func runCorpusSignalRefreshLoop(ctx context.Context, refresh *corpusSignalRefresh) {
	if refresh == nil {
		return
	}
	if refresh.initialRefreshDelay > 0 && !refresh.waitFor(ctx, refresh.initialRefreshDelay) {
		return
	}
	for ctx.Err() == nil {
		refresh.scanAndPublish(ctx)
		if !refresh.wait(ctx) {
			return
		}
	}
}

func (r *corpusSignalRefresh) wait(ctx context.Context) bool {
	return r.waitFor(ctx, defaultCorpusSignalRefreshInterval)
}

func (r *corpusSignalRefresh) waitFor(ctx context.Context, interval time.Duration) bool {
	delay, stop := newCorpusSignalRefreshDelay(interval)
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
	hostLinkAccumulator := hostLinkAccumulator{incoming: map[string]map[string]hostLinkReference{}}
	err := scanCorpusSignalDocuments(
		ctx,
		r.documents,
		func(doc documentstore.Document) (bool, error) {
			visitDocumentAuthorityCitations(doc, func(citation hostrank.Citation) {
				citationSample.Add(citation)
			})
			collectDocumentHostLinks(&hostLinkAccumulator, doc)
			spellcheck.ObserveTextFrequencies(doc.Title, spellFrequency, wordFormsFrequency)
			spellcheck.ObserveTextFrequencies(doc.ExtractedText, spellFrequency, wordFormsFrequency)

			return true, nil
		},
	)
	if err != nil {
		slog.WarnContext(ctx, corpusSignalRefreshFailedMessage, slog.Any("error", err))

		return
	}

	citations := citationSample.Citations()
	trustPolicy := r.currentTrustPolicy()
	authority := hostrank.AuthorityTable{}
	if r.hostRank != nil {
		table, err := r.computeAuthority(ctx, citations, trustPolicy)
		if err != nil {
			slog.WarnContext(ctx, hostAuthorityRefreshFailedMessage, slog.Any("error", err))

			return
		}
		authority = table
	}
	spellingVocabulary := prunedVocabulary(spellFrequency.Frequencies())
	wordFormsVocabulary := map[string]int{}
	wordFormsReady := wordFormsFrequency != nil
	if wordFormsReady {
		wordFormsVocabulary = wordFormsFrequency.Frequencies()
	}
	hostLinkGraph := hostlinks.Graph{
		RowDefinition: hostlinks.HostReferenceRowDefinition,
		LinkedHosts:   hostLinkGraphHosts(hostLinkAccumulator.incoming),
	}
	checkpoint := r.checkpointWithTrustPolicy(corpussignals.Checkpoint{
		Authority: authority, Citations: citations, Spelling: spellingVocabulary,
		WordForms: wordFormsVocabulary, WordFormsReady: wordFormsReady,
		HostLinks: hostLinkGraph, HostLinksReady: true,
		CompletedAtUnixMilli: r.currentTime().UnixMilli(),
	}, trustPolicy)
	if ctx.Err() != nil {
		return
	}
	r.persistCheckpoint(ctx, checkpoint)
	r.acceptCheckpoint(checkpoint)
}

func (r *corpusSignalRefresh) publishAuthority(ctx context.Context) {
	if r.hostRank == nil || !r.citationsReady {
		return
	}
	trustPolicy := r.currentTrustPolicy()
	table, err := r.computeAuthority(ctx, r.citations, trustPolicy)
	if err != nil {
		slog.WarnContext(ctx, hostAuthorityRefreshFailedMessage, slog.Any("error", err))

		return
	}
	checkpoint := r.checkpointWithTrustPolicy(corpussignals.Checkpoint{
		Authority: table, Citations: r.citations, Spelling: r.spellingVocabulary,
		WordForms: r.wordFormsVocabulary, WordFormsReady: r.wordFormsReady,
		HostLinks: r.hostLinkGraph, HostLinksReady: r.hostLinksReady,
		CompletedAtUnixMilli: r.completedAtUnixMilli,
	}, trustPolicy)
	r.persistCheckpoint(ctx, checkpoint)
	r.hostRank.Store(table)
}

func (r *corpusSignalRefresh) computeAuthority(
	ctx context.Context,
	citations []hostrank.Citation,
	policy hosttrust.Policy,
) (hostrank.AuthorityTable, error) {
	table, err := hostrank.ComputeDomainAuthority(ctx, citations, hostrank.DomainOptions{
		TrustedDomains: policy.Domains,
		TrustBlend:     policy.Blend,
	})
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

func (r *corpusSignalRefresh) currentTime() time.Time {
	if r.readTime != nil {
		return r.readTime()
	}

	return time.Now()
}
