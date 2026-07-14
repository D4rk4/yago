package yagonode

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/corpussignals"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

const (
	corpusSignalCheckpointLoadFailedMessage    = "corpus signal checkpoint load failed"
	corpusSignalCheckpointPersistFailedMessage = "corpus signal checkpoint persistence failed"
)

type corpusSignalCheckpointRepository interface {
	Load(context.Context) (corpussignals.Checkpoint, bool, error)
	Replace(context.Context, corpussignals.Checkpoint) error
}

type corpusSignalAssembly struct {
	documents        documentstore.StoredDocuments
	authority        *hostrank.Holder
	spelling         *spellcheck.Holder
	wordForms        *wordforms.Holder
	includeWordForms bool
	trust            hostTrustPolicySource
	checkpoint       corpusSignalCheckpointRepository
}

func newCorpusSignalRefresh(
	ctx context.Context,
	assembly corpusSignalAssembly,
) *corpusSignalRefresh {
	refresh := &corpusSignalRefresh{
		documents: assembly.documents, hostRank: assembly.authority,
		spell: assembly.spelling, wordForms: assembly.wordForms,
		includeWordForms: assembly.includeWordForms, trust: assembly.trust,
		checkpoints: assembly.checkpoint,
		readTime:    time.Now,
	}
	refresh.initialRefreshDelay = refresh.restoreCheckpoint(ctx)

	return refresh
}

func corpusSignalRefreshForNode(ctx context.Context, assembled node) *corpusSignalRefresh {
	if assembled.corpusPass != nil {
		return assembled.corpusPass
	}

	return newCorpusSignalRefresh(
		ctx,
		corpusSignalAssembly{
			documents: assembled.docScan, authority: assembled.hostRank,
			spelling: assembled.spell, wordForms: assembled.wordForms,
			includeWordForms: assembled.swarmMorph, trust: assembled.hostTrust,
		},
	)
}

func (r *corpusSignalRefresh) restoreCheckpoint(ctx context.Context) time.Duration {
	if r.checkpoints == nil {
		return 0
	}
	checkpoint, found, err := r.checkpoints.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, corpusSignalCheckpointLoadFailedMessage, slog.Any("error", err))

		return 0
	}
	if !found {
		return 0
	}
	trustPolicy := r.currentTrustPolicy()
	if !slices.Equal(checkpoint.TrustDomains, trustPolicy.Domains) ||
		checkpoint.TrustBlend != trustPolicy.Blend {
		authority, computeErr := r.computeAuthority(ctx, checkpoint.Citations, trustPolicy)
		if computeErr != nil {
			slog.WarnContext(ctx, hostAuthorityRefreshFailedMessage, slog.Any("error", computeErr))

			return 0
		}
		checkpoint.Authority = authority
		checkpoint.TrustDomains = trustPolicy.Domains
		checkpoint.TrustBlend = trustPolicy.Blend
		r.persistCheckpoint(ctx, checkpoint)
	}
	r.acceptCheckpoint(checkpoint)
	if r.includeWordForms && !checkpoint.WordFormsReady {
		return 0
	}

	return remainingCorpusSignalRefreshDelay(
		r.currentTime(),
		checkpoint.CompletedAtUnixMilli,
	)
}

func (r *corpusSignalRefresh) acceptCheckpoint(checkpoint corpussignals.Checkpoint) {
	r.citations = checkpoint.Citations
	r.citationsReady = true
	r.spellingVocabulary = checkpoint.Spelling
	r.wordFormsVocabulary = checkpoint.WordForms
	r.wordFormsReady = checkpoint.WordFormsReady
	r.completedAtUnixMilli = checkpoint.CompletedAtUnixMilli
	if r.hostRank != nil {
		r.hostRank.Store(checkpoint.Authority)
	}
	if r.spell != nil {
		r.spell.Store(spellcheck.New(checkpoint.Spelling))
	}
	if r.wordForms != nil && r.includeWordForms && checkpoint.WordFormsReady {
		r.wordForms.Store(wordforms.New(checkpoint.WordForms, searchindex.StemWord))
	}
}

func (r *corpusSignalRefresh) persistCheckpoint(
	ctx context.Context,
	checkpoint corpussignals.Checkpoint,
) {
	if r.checkpoints == nil {
		return
	}
	if err := r.checkpoints.Replace(ctx, checkpoint); err != nil {
		slog.WarnContext(
			ctx,
			corpusSignalCheckpointPersistFailedMessage,
			slog.Any("error", err),
		)
	}
}

func (r *corpusSignalRefresh) checkpointWithTrustPolicy(
	checkpoint corpussignals.Checkpoint,
	policy hosttrust.Policy,
) corpussignals.Checkpoint {
	checkpoint.TrustDomains = policy.Domains
	checkpoint.TrustBlend = policy.Blend

	return checkpoint
}

type corpusSignalSet struct {
	authority *hostrank.Holder
	spelling  *spellcheck.Holder
	wordForms *wordforms.Holder
	refresh   *corpusSignalRefresh
}

func newCorpusSignalSet(
	in assembleSurfacesInput,
	learning searchLearningStores,
) corpusSignalSet {
	set := corpusSignalSet{
		authority: hostrank.NewHolder(),
		spelling:  spellcheck.NewHolder(),
		wordForms: wordforms.NewHolder(),
	}
	set.refresh = newCorpusSignalRefresh(in.ctx, corpusSignalAssembly{
		documents: in.storage.storedDocuments(), authority: set.authority,
		spelling: set.spelling, wordForms: set.wordForms,
		includeWordForms: in.config.SwarmMorphology, trust: learning.trust,
		checkpoint: learning.checkpoint,
	})

	return set
}

func remainingCorpusSignalRefreshDelay(now time.Time, completedAtUnixMilli int64) time.Duration {
	completedAt := time.UnixMilli(completedAtUnixMilli)
	if completedAt.After(now) {
		return 0
	}
	age := now.Sub(completedAt)
	if age >= defaultCorpusSignalRefreshInterval {
		return 0
	}

	return defaultCorpusSignalRefreshInterval - age
}

func (r *corpusSignalRefresh) currentTrustPolicy() hosttrust.Policy {
	if r.trust == nil {
		return hosttrust.Policy{Domains: []string{}}
	}

	return r.trust.Current()
}
