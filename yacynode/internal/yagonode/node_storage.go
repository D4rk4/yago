package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/searchindex"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yacynode/internal/urlreferences"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func (s nodeStorage) storedDocuments() documentstore.StoredDocuments {
	stored, _ := s.documentDirectory.(documentstore.StoredDocuments)

	return stored
}

type nodeStorage struct {
	documentDirectory documentstore.DocumentDirectory
	documentReceiver  documentstore.DocumentReceiver
	searchIndex       searchindex.SearchIndex
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
	openSearchIndex      = func(
		ctx context.Context,
		path string,
		documents documentstore.DocumentDirectory,
	) (searchindex.SearchIndex, error) {
		stored, _ := documents.(documentstore.StoredDocuments)
		if strings.TrimSpace(path) == "" {
			return searchindex.NewBleveMemoryIndex(ctx, stored)
		}

		return searchindex.NewBleveDiskIndex(ctx, path, documents, stored)
	}
	openURLMetadata   = urlmeta.Open
	openURLReferences = urlreferences.Open
	openRWIStorage    = rwi.Open
)

func openNodeStorage(vault *vault.Vault, searchIndexPath string) (nodeStorage, error) {
	documentDirectory, documentReceiver, err := openDocuments(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("document storage: %w", err)
	}

	searchIndex, err := openSearchIndex(context.Background(), searchIndexPath, documentDirectory)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("search index: %w", err)
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
		searchIndex:       searchIndex,
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
