package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yacynode/internal/urlreferences"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type nodeStorage struct {
	documentDirectory documentstore.DocumentDirectory
	documentReceiver  documentstore.DocumentReceiver
	urlDirectory      urlmeta.URLDirectory
	urlMetadataRows   urlmeta.StoredURLMetadataRows
	urlEvictor        urlmeta.URLEvictor
	urlReceiver       urlmeta.URLReceiver
	staleness         urlmetastaleness.StalenessRanking
	references        urlreferences.ReferenceProjection
	postings          rwi.PostingIndex
	outboundPostings  rwi.OutboundPostingStore
	postingReceiver   rwi.PostingReceiver
	postingPurger     rwi.PostingPurger
}

var (
	openStalenessRanking = urlmetastaleness.Open
	openDocuments        = documentstore.Open
	openURLMetadata      = urlmeta.Open
	openURLReferences    = urlreferences.Open
	openRWIStorage       = rwi.Open
)

func openNodeStorage(vault *vault.Vault) (nodeStorage, error) {
	documentDirectory, documentReceiver, err := openDocuments(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("document storage: %w", err)
	}

	staleness, err := openStalenessRanking(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("url metadata staleness: %w", err)
	}

	urlDirectory, urlEvictor, urlReceiver, err := openURLMetadata(vault, staleness)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("urlmeta storage: %w", err)
	}
	urlMetadataRows, _ := urlDirectory.(urlmeta.StoredURLMetadataRows)

	references, err := openURLReferences(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("url references: %w", err)
	}

	postings, postingReceiver, postingPurger, err := openRWIStorage(
		vault,
		urlDirectory,
		rwi.Config{BatchCap: receiveBatchCap, PauseSeconds: receiveBusyPauseSecs},
		references,
	)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("rwi storage: %w", err)
	}
	outboundPostings, ok := postings.(rwi.OutboundPostingStore)
	if !ok {
		return nodeStorage{}, fmt.Errorf("rwi outbound storage unavailable")
	}
	if _, err := outboundPostings.RecoverOutbound(context.Background()); err != nil {
		return nodeStorage{}, fmt.Errorf("recover outbound rwi: %w", err)
	}

	return nodeStorage{
		documentDirectory: documentDirectory,
		documentReceiver:  documentReceiver,
		urlDirectory:      urlDirectory,
		urlMetadataRows:   urlMetadataRows,
		urlEvictor:        urlEvictor,
		urlReceiver:       urlReceiver,
		staleness:         staleness,
		references:        references,
		postings:          postings,
		outboundPostings:  outboundPostings,
		postingReceiver:   postingReceiver,
		postingPurger:     postingPurger,
	}, nil
}
