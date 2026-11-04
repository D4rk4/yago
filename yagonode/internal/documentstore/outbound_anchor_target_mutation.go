package documentstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	outboundAnchorTargetPageSize              = 16
	outboundAnchorMutationMaximumRows         = 32
	outboundAnchorMutationMaximumEncodedBytes = 8 << 20
)

type outboundAnchorTargetMutation struct {
	anchors          []AnchorText
	clearRefresh     bool
	deleteAnchors    bool
	document         Document
	documentLocation storedDocumentLocation
	encodedBytes     int
	refresh          *inboundAnchorRefresh
	storeAnchors     bool
	storeDocument    bool
	targetURL        string
}

func (d documentVault) prepareOutboundAnchorTargetMutations(
	ctx context.Context,
	replacement outboundAnchorDocumentReplacement,
	targetURLs []string,
) ([]outboundAnchorTargetMutation, error) {
	mutations := make([]outboundAnchorTargetMutation, 0, len(targetURLs))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, targetURL := range targetURLs {
			mutation, err := d.prepareOutboundAnchorTargetMutation(
				tx,
				replacement,
				targetURL,
			)
			if err != nil {
				return err
			}
			mutations = append(mutations, mutation)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("prepare outbound anchor target mutations: %w", err)
	}

	return mutations, nil
}

func (d documentVault) prepareOutboundAnchorTargetMutation(
	tx *vault.Txn,
	replacement outboundAnchorDocumentReplacement,
	targetURL string,
) (outboundAnchorTargetMutation, error) {
	document, _, documentFound, err := d.readStoredDocument(tx, targetURL)
	if err != nil {
		return outboundAnchorTargetMutation{}, fmt.Errorf(
			"read anchor target document: %w",
			err,
		)
	}
	anchors, anchorsFound, err := d.inboundAnchors.Get(tx, vault.Key(targetURL))
	if err != nil {
		return outboundAnchorTargetMutation{}, fmt.Errorf("read target anchors: %w", err)
	}
	current, pending, err := d.inlinksAfterPendingRefresh(
		tx,
		targetURL,
		document.Inlinks,
		anchors,
	)
	if err != nil {
		return outboundAnchorTargetMutation{}, err
	}
	if !anchorsFound && documentFound {
		anchors = current
	}
	mutation := replacedTargetAnchors(replacement, targetURL, anchors, anchorsFound)
	inlinks := inlinksAfterAnchorReplacement(
		current,
		anchors,
		replacement.sources,
		mutation.anchors,
	)
	if documentFound && !slices.Equal(current, inlinks) {
		refresh := pending.merged(*inboundAnchorRefreshFor(replacement.sources))
		mutation.refresh = &refresh
	}
	mutation.encodedBytes = outboundAnchorTargetMutationEncodedBytes(mutation)

	return mutation, nil
}

func outboundAnchorTargetMutationEncodedBytes(
	mutation outboundAnchorTargetMutation,
) int {
	encodedBytes := 0
	if mutation.deleteAnchors {
		encodedBytes += len(mutation.targetURL)
	}
	if mutation.clearRefresh {
		encodedBytes += len(mutation.targetURL)
	}
	if mutation.storeAnchors {
		raw, _ := (anchorJSONCodec[[]AnchorText]{}).Encode(mutation.anchors)
		encodedBytes += len(mutation.targetURL) + len(raw)
	}
	encodedBytes += mutation.refresh.encodedBytes(mutation.targetURL)
	if mutation.storeDocument {
		keyBytes := len(mutation.targetURL)
		if mutation.documentLocation.admission > 0 {
			keyBytes += orderedDocumentAdmissionSize
		}
		raw, _ := (documentCodec{}).Encode(mutation.document)
		encodedBytes += keyBytes + len(raw)
	}

	return encodedBytes
}

func (d documentVault) storeOutboundAnchorTargetMutation(
	tx *vault.Txn,
	mutation outboundAnchorTargetMutation,
) error {
	key := vault.Key(mutation.targetURL)
	if mutation.deleteAnchors {
		if _, err := d.inboundAnchors.Delete(tx, key); err != nil {
			return fmt.Errorf("delete target anchors: %w", err)
		}
	}
	if mutation.storeAnchors {
		if err := d.inboundAnchors.Put(tx, key, mutation.anchors); err != nil {
			return fmt.Errorf("store target anchors: %w", err)
		}
	}
	if mutation.storeDocument {
		if err := d.putStoredDocument(
			tx,
			mutation.documentLocation,
			mutation.document,
		); err != nil {
			return fmt.Errorf("store anchor target document: %w", err)
		}
	}
	if mutation.clearRefresh {
		if _, err := d.inboundAnchorRefreshes.Delete(tx, key); err != nil {
			return fmt.Errorf("clear inbound anchor refresh: %w", err)
		}
	}

	return d.storeInboundAnchorRefresh(tx, mutation.targetURL, mutation.refresh)
}

func (mutation outboundAnchorTargetMutation) storageRows() int {
	rows := 0
	if mutation.clearRefresh {
		rows++
	}
	if mutation.deleteAnchors {
		rows++
	}
	if mutation.storeAnchors {
		rows++
	}
	if mutation.storeDocument {
		rows++
	}
	if mutation.refresh != nil {
		rows++
	}

	return rows
}
