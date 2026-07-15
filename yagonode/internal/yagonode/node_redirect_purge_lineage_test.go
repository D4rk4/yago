package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/redirectpurge"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type redirectPurgePostingScript struct {
	purged int
}

func (s *redirectPurgePostingScript) PurgePosting(
	*vault.Txn,
	yagomodel.Hash,
	yagomodel.Hash,
) (bool, error) {
	s.purged++

	return true, nil
}

type redirectPurgeReferenceScript struct{}

func (redirectPurgeReferenceScript) WordsReferencing(
	*vault.Txn,
	yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return []yagomodel.Hash{yagomodel.WordHash("word")}, nil
}

func (redirectPurgeReferenceScript) ReferencedURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (redirectPurgeReferenceScript) ReferencedURLCount(context.Context) (int, error) {
	return 0, nil
}

type redirectPurgeMetadataScript struct {
	purged int
}

func (s *redirectPurgeMetadataScript) Purge(
	_ context.Context,
	_ *vault.Txn,
	urls []yagomodel.Hash,
) (urlmeta.PurgeResult, error) {
	s.purged += len(urls)

	return urlmeta.PurgeResult{URLsDeleted: len(urls)}, nil
}

type redirectPurgeClusterScript struct {
	durableLineageClusterScript
	transitions int
}

func (s *redirectPurgeClusterScript) DeleteTransition(
	ctx context.Context,
	normalizedURL string,
) (contentcluster.EvidenceDeletion, error) {
	s.transitions++

	return s.durableLineageClusterScript.DeleteTransition(ctx, normalizedURL)
}

func TestRedirectPurgeUsesOneCompleteDocumentLineageOwner(t *testing.T) {
	sourceURL := "https://www.bing.com/ck/a?u=a1source"
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		sourceURL: {NormalizedURL: sourceURL},
	}}
	anchors := &lineageAnchorScript{}
	clusters := &redirectPurgeClusterScript{}
	index := &lineageIndexScript{}
	postings := &redirectPurgePostingScript{}
	metadata := &redirectPurgeMetadataScript{}
	lineages := documentLineageEvictor{
		directory: documents,
		documents: documents,
		anchors:   anchors,
		clusters:  clusters,
		index:     index,
	}
	purger := eviction.NewEvictor(
		openTestVault(t),
		postings,
		redirectPurgeReferenceScript{},
		metadata,
		lineages,
		nil,
	)
	redirectpurge.New(
		fakeStoredDocuments{docs: []documentstore.Document{{NormalizedURL: sourceURL}}},
		purger.PurgeResolved,
	).Run(t.Context())

	if len(index.deleted) != 1 || len(documents.deleted) != 1 ||
		len(anchors.sets) != 1 || clusters.transitions != 1 ||
		clusters.finalized != 1 || clusters.released != 1 ||
		postings.purged != 1 || metadata.purged != 1 {
		t.Fatalf(
			"lineage ownership = index:%v documents:%v anchors:%d clusters:%d/%d/%d rwi:%d metadata:%d",
			index.deleted,
			documents.deleted,
			len(anchors.sets),
			clusters.transitions,
			clusters.finalized,
			clusters.released,
			postings.purged,
			metadata.purged,
		)
	}
}

func TestRedirectPurgeKeepsRWIAndMetadataAfterLineageFailure(t *testing.T) {
	sourceURL := "https://www.bing.com/ck/a?u=a1source"
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		sourceURL: {NormalizedURL: sourceURL},
	}}
	clusters := &redirectPurgeClusterScript{}
	index := &lineageIndexScript{deleteErr: errors.New("index failed")}
	postings := &redirectPurgePostingScript{}
	metadata := &redirectPurgeMetadataScript{}
	lineages := documentLineageEvictor{
		directory: documents,
		documents: documents,
		anchors:   &lineageAnchorScript{},
		clusters:  clusters,
		index:     index,
	}
	purger := eviction.NewEvictor(
		openTestVault(t),
		postings,
		redirectPurgeReferenceScript{},
		metadata,
		lineages,
		nil,
	)
	redirectpurge.New(
		fakeStoredDocuments{docs: []documentstore.Document{{NormalizedURL: sourceURL}}},
		purger.PurgeResolved,
	).Run(t.Context())

	if len(documents.deleted) != 1 || len(index.deleted) != 1 ||
		clusters.transitions != 1 || clusters.finalized != 0 ||
		clusters.released != 1 || postings.purged != 0 || metadata.purged != 0 {
		t.Fatalf(
			"failed lineage = documents:%v index:%v clusters:%d/%d/%d rwi:%d metadata:%d",
			documents.deleted,
			index.deleted,
			clusters.transitions,
			clusters.finalized,
			clusters.released,
			postings.purged,
			metadata.purged,
		)
	}
}

func TestNewNodeRedirectPurgeRejectsIncompleteStorage(t *testing.T) {
	if newNodeRedirectPurge(nodeStorage{}, nil) != nil {
		t.Fatal("incomplete redirect purge storage was enabled")
	}
}
