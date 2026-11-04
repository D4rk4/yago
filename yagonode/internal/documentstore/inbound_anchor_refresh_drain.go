package documentstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type InboundAnchorRefreshReceipt struct {
	Refreshed int
	More      bool
}

type InboundAnchorDocumentRefresher interface {
	RefreshInboundAnchorDocuments(
		context.Context,
		int,
		func([]Document) error,
	) (InboundAnchorRefreshReceipt, error)
}

func (d documentVault) RefreshInboundAnchorDocuments(
	ctx context.Context,
	limit int,
	reindex func([]Document) error,
) (InboundAnchorRefreshReceipt, error) {
	var page vault.BucketKeyPage
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		page, err = tx.ReadBucketKeyPage(inboundAnchorRefreshBucket, nil, limit)
		if err != nil {
			return fmt.Errorf("page inbound anchor refresh keys: %w", err)
		}

		return nil
	})
	if err != nil {
		return InboundAnchorRefreshReceipt{}, fmt.Errorf("read inbound anchor refreshes: %w", err)
	}
	if len(page.Keys) == 0 {
		return InboundAnchorRefreshReceipt{}, nil
	}
	targetURLs := make([]string, 0, len(page.Keys))
	for _, key := range page.Keys {
		targetURLs = append(targetURLs, string(key))
	}
	refreshed, err := d.refreshInboundAnchorDocuments(ctx, targetURLs, reindex)
	if err != nil {
		return InboundAnchorRefreshReceipt{}, err
	}

	return InboundAnchorRefreshReceipt{Refreshed: refreshed, More: page.More}, nil
}

func (d documentVault) refreshInboundAnchorDocuments(
	ctx context.Context,
	targetURLs []string,
	reindex func([]Document) error,
) (int, error) {
	releaseWrite, err := d.enterStoredDocumentWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer releaseWrite()
	releaseTargets, err := d.urlBoundaries.lockWrites(ctx, targetURLs)
	if err != nil {
		return 0, err
	}
	defer releaseTargets()
	mutations := make([]outboundAnchorTargetMutation, 0, len(targetURLs))
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, targetURL := range targetURLs {
			mutation, err := d.prepareInboundAnchorRefreshMutation(tx, targetURL)
			if err != nil {
				return err
			}
			mutations = append(mutations, mutation)
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("prepare inbound anchor refreshes: %w", err)
	}
	documents := make([]Document, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.storeDocument {
			documents = append(documents, normalizedDocument(mutation.document))
		}
	}
	if len(documents) > 0 {
		if err := reindex(documents); err != nil {
			return 0, fmt.Errorf("reindex inbound anchor documents: %w", err)
		}
	}
	if err := d.storeOutboundAnchorTargetMutations(ctx, mutations); err != nil {
		return 0, err
	}
	if err := d.clearInboundAnchorRefreshes(ctx, targetURLs); err != nil {
		return 0, err
	}

	return len(documents), nil
}

func (d documentVault) prepareInboundAnchorRefreshMutation(
	tx *vault.Txn,
	targetURL string,
) (outboundAnchorTargetMutation, error) {
	mutation := outboundAnchorTargetMutation{targetURL: targetURL}
	document, location, documentFound, err := d.readStoredDocument(tx, targetURL)
	if err != nil {
		return outboundAnchorTargetMutation{}, fmt.Errorf("read refreshed document: %w", err)
	}
	key := vault.Key(targetURL)
	refresh, _, err := d.inboundAnchorRefreshes.Get(tx, key)
	if err != nil {
		return outboundAnchorTargetMutation{}, fmt.Errorf("read inbound anchor refresh: %w", err)
	}
	materialized, _, err := d.inboundAnchors.Get(tx, key)
	if err != nil {
		return outboundAnchorTargetMutation{}, fmt.Errorf("read refreshed anchors: %w", err)
	}
	inlinks := refresh.refreshedInlinks(document.Inlinks, materialized)
	if documentFound && !slices.Equal(document.Inlinks, inlinks) {
		document.Inlinks = inlinks
		mutation.document = document
		mutation.documentLocation = location
		mutation.storeDocument = true
	}
	mutation.encodedBytes = outboundAnchorTargetMutationEncodedBytes(mutation)

	return mutation, nil
}
