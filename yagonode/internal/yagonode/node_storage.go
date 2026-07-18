package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (s nodeStorage) storedDocuments() documentstore.StoredDocuments {
	stored, _ := s.documentDirectory.(documentstore.StoredDocuments)

	return stored
}

// documentEvictor exposes the document store's delete side so a URL purge can
// drop the document alongside the postings and metadata (ADR-0036 B).
func (s nodeStorage) documentEvictor() documentstore.DocumentEvictor {
	return newDocumentLineageEvictor(s)
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
	contentClusters   *contentcluster.Index
}

var (
	openStalenessRanking = urlmetastaleness.Open
	openDocuments        = documentstore.Open
	openContentClusters  = contentcluster.Open
	openSearchIndex      = func(
		ctx context.Context,
		path string,
		documents documentstore.DocumentDirectory,
		rebuildAdmissions ...searchindex.BleveRebuildGrowthAdmission,
	) (searchindex.SearchIndex, error) {
		stored, _ := documents.(documentstore.StoredDocuments)
		if strings.TrimSpace(path) == "" {
			return searchindex.NewBleveMemoryIndex(ctx, stored)
		}

		return searchindex.NewBleveDiskIndex(
			ctx,
			path,
			documents,
			stored,
			rebuildAdmissions...,
		)
	}
	openURLMetadata   = urlmeta.Open
	openURLReferences = urlreferences.Open
	openRWIStorage    = rwi.Open
)

func openNodeStorage(
	vault *vault.Vault,
	searchIndexPath string,
	admissions ...growthAdmission,
) (nodeStorage, error) {
	documentDirectory, documentReceiver, err := openDocuments(vault)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("document storage: %w", err)
	}
	contentClusters, err := openContentClusters(vault, contentcluster.Limits{})
	if err != nil {
		return nodeStorage{}, fmt.Errorf("content clusters: %w", err)
	}

	var rebuildAdmission searchindex.BleveRebuildGrowthAdmission
	if len(admissions) > 0 {
		rebuildAdmission = admissions[0]
	}
	searchIndex, err := openSearchIndex(
		context.Background(),
		searchIndexPath,
		documentDirectory,
		rebuildAdmission,
	)
	if err != nil {
		return nodeStorage{}, fmt.Errorf("search index: %w", err)
	}
	searchIndex = searchindex.NewCachedSearchIndex(searchIndex, 0)

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
		rwi.Config{
			BatchCap:          receiveBatchCap,
			PauseMilliseconds: receiveBusyPauseMilliseconds,
		},
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
		contentClusters:   contentClusters,
	}, nil
}
