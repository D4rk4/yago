package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
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
) (searchLearningStores, error) {
	judgmentStore, err := judgments.Open(storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open search judgments: %w", err)
	}
	clickStore, err := clickcapture.Open(storage)
	if err != nil {
		return searchLearningStores{}, fmt.Errorf("open search clicks: %w", err)
	}
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

	return searchLearningStores{
		judgments:  judgmentStore,
		clicks:     clickStore,
		models:     models,
		safety:     safety,
		trust:      trust,
		reputation: peerEvents,
		peerEvents: peerEvents,
	}, nil
}
