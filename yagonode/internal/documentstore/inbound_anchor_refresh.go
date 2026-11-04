package documentstore

import (
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	inboundAnchorRefreshBucket         vault.Name = "document_inbound_anchor_refreshes"
	maximumInboundAnchorRefreshSources            = maximumInboundAnchors
)

type inboundAnchorRefresh struct {
	ReplacedSources    []string `json:"replaced_sources,omitempty"`
	AllSourcesReplaced bool     `json:"all_sources_replaced,omitempty"`
}

func registerInboundAnchorRefreshes(
	v *vault.Vault,
) (*vault.Keyspace[inboundAnchorRefresh], error) {
	refreshes, err := vault.RegisterKeyspace(
		v,
		inboundAnchorRefreshBucket,
		anchorJSONCodec[inboundAnchorRefresh]{},
	)
	if err != nil {
		return nil, fmt.Errorf("register inbound anchor refreshes: %w", err)
	}

	return refreshes, nil
}

func inboundAnchorRefreshFor(sources map[string]struct{}) *inboundAnchorRefresh {
	replaced := make([]string, 0, len(sources))
	for source := range sources {
		replaced = append(replaced, source)
	}
	refresh := inboundAnchorRefresh{}.merged(inboundAnchorRefresh{ReplacedSources: replaced})

	return &refresh
}

func (r inboundAnchorRefresh) merged(other inboundAnchorRefresh) inboundAnchorRefresh {
	if r.AllSourcesReplaced || other.AllSourcesReplaced {
		return inboundAnchorRefresh{AllSourcesReplaced: true}
	}
	sources := uniqueSortedStrings(
		append(slices.Clone(r.ReplacedSources), other.ReplacedSources...),
	)
	if len(sources) > maximumInboundAnchorRefreshSources {
		return inboundAnchorRefresh{AllSourcesReplaced: true}
	}

	return inboundAnchorRefresh{ReplacedSources: sources}
}

func (r inboundAnchorRefresh) refreshedInlinks(
	inlinks []AnchorText,
	materialized []AnchorText,
) []AnchorText {
	refreshed := slices.Clone(materialized)
	if !r.AllSourcesReplaced {
		for _, anchor := range inlinks {
			if _, replaced := slices.BinarySearch(r.ReplacedSources, anchor.URL); !replaced {
				refreshed = append(refreshed, anchor)
			}
		}
	}

	return canonicalAnchorTexts(refreshed)
}

func (d documentVault) inlinksAfterPendingRefresh(
	tx *vault.Txn,
	targetURL string,
	inlinks []AnchorText,
	materialized []AnchorText,
) ([]AnchorText, inboundAnchorRefresh, error) {
	refresh, pending, err := d.inboundAnchorRefreshes.Get(tx, vault.Key(targetURL))
	if err != nil {
		return nil, inboundAnchorRefresh{}, fmt.Errorf("read inbound anchor refresh: %w", err)
	}
	if !pending {
		return inlinks, refresh, nil
	}

	return refresh.refreshedInlinks(inlinks, materialized), refresh, nil
}

func (r *inboundAnchorRefresh) encodedBytes(targetURL string) int {
	if r == nil {
		return 0
	}
	raw, _ := (anchorJSONCodec[inboundAnchorRefresh]{}).Encode(*r)

	return len(targetURL) + len(raw)
}

func (d documentVault) storeInboundAnchorRefresh(
	tx *vault.Txn,
	targetURL string,
	refresh *inboundAnchorRefresh,
) error {
	if refresh == nil {
		return nil
	}
	if err := d.inboundAnchorRefreshes.Put(tx, vault.Key(targetURL), *refresh); err != nil {
		return fmt.Errorf("store inbound anchor refresh: %w", err)
	}

	return nil
}
