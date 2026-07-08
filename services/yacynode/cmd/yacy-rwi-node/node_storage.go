package main

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmetastaleness"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlreferences"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type nodeStorage struct {
	urlDirectory    urlmeta.URLDirectory
	urlEvictor      urlmeta.URLEvictor
	urlReceiver     urlmeta.URLReceiver
	staleness       urlmetastaleness.StalenessRanking
	references      urlreferences.ReferenceProjection
	postings        rwi.PostingIndex
	postingReceiver rwi.PostingReceiver
	postingPurger   rwi.PostingPurger
}

func openNodeStorage(vault *vault.Vault) (nodeStorage, error) {
	staleness, err := urlmetastaleness.Open(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("url metadata staleness: %w", err)
	}

	urlDirectory, urlEvictor, urlReceiver, err := urlmeta.Open(vault, staleness)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("urlmeta storage: %w", err)
	}

	references, err := urlreferences.Open(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("url references: %w", err)
	}

	postings, postingReceiver, postingPurger, err := rwi.Open(
		vault,
		urlDirectory,
		rwi.Config{BatchCap: receiveBatchCap, PauseSeconds: receiveBusyPauseSecs},
		references,
	)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("rwi storage: %w", err)
	}

	return nodeStorage{
		urlDirectory:    urlDirectory,
		urlEvictor:      urlEvictor,
		urlReceiver:     urlReceiver,
		staleness:       staleness,
		references:      references,
		postings:        postings,
		postingReceiver: postingReceiver,
		postingPurger:   postingPurger,
	}, nil
}
