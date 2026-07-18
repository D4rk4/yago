package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/corpussignals"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
	"github.com/D4rk4/yago/yagonode/internal/safetymodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type searchLearningStores struct {
	judgments  *judgments.Store
	clicks     *clickcapture.Store
	models     *rankingmodel.Catalog
	safety     *safetymodel.Catalog
	trust      *hosttrust.Catalog
	checkpoint *corpussignals.CheckpointRepository
	reputation *peerReputationObserver
	peerEvents *peerReputationObserver
}

var (
	openRuntimePeerReputation = peerreputation.Open
	newRuntimePeerObserver    = newPeerReputationObserver
	openRuntimeHostTrust      = hosttrust.Open
)

func openSearchLearningStores(
	ctx context.Context,
	storage *vault.Vault,
	admissions ...growthAdmission,
) (searchLearningStores, error) {
	judgmentStore, err := judgments.Open(storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open search judgments: %w", err)
	}
	clickStore, err := clickcapture.Open(storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open search clicks: %w", err)
	}
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}
	clickStore.AdmitImpressionGrowth(admission)
	models, err := rankingmodel.Open(ctx, storage, learnedrank.DefaultCandidateWindow)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open ranking models: %w", err)
	}
	safety, err := safetymodel.Open(ctx, storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open content safety models: %w", err)
	}
	trust, err := openRuntimeHostTrust(ctx, storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open host trust catalog: %w", err)
	}
	checkpoints, err := corpussignals.Open(storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open corpus signal checkpoints: %w", err)
	}
	reputation, err := openRuntimePeerReputation(
		storage,
		peerreputation.DefaultConfiguration(),
	)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open peer reputation: %w", err)
	}
	peerEvents, err := newRuntimePeerObserver(ctx, reputation)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open peer reputation observations: %w", err)
	}
	peerEvents.growthAdmission = admission

	return searchLearningStores{
		judgments:  judgmentStore,
		clicks:     clickStore,
		models:     models,
		safety:     safety,
		trust:      trust,
		checkpoint: checkpoints,
		reputation: peerEvents,
		peerEvents: peerEvents,
	}, nil
}
