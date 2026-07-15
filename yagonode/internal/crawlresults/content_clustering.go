package crawlresults

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type ContentClusters interface {
	Replace(context.Context, contentcluster.Evidence) (contentcluster.Assignment, error)
	Delete(context.Context, string) (bool, error)
	Lookup(context.Context, string) (contentcluster.Assignment, bool, error)
	Cluster(context.Context, string) (contentcluster.Cluster, bool, error)
}

type documentClusterReplacement struct {
	documents          []documentstore.Document
	currentURLs        map[string]struct{}
	affectedClusterIDs map[string]struct{}
	assignedClusterIDs map[string]struct{}
	finalizations      []contentcluster.EvidenceFinalization
	transitions        contentClusterTransitionFinalizer
	replay             bool
}

func (c *IngestConsumer) TrackContentClusters(clusters ContentClusters) {
	if clusters != nil {
		c.clusters = clusters
	}
}

func (c *IngestConsumer) prepareDocumentClusters(
	ctx context.Context,
	docs []documentstore.Document,
) (documentClusterProjection, error) {
	if c.clusters == nil || len(docs) == 0 {
		return documentClusterProjection{documents: docs}, nil
	}
	replacement, err := c.replaceDocumentClusters(ctx, docs)
	if err != nil {
		return documentClusterProjection{}, err
	}
	projection := documentClusterProjection{
		finalizations: replacement.finalizations,
		transitions:   replacement.transitions,
		replay:        replacement.replay,
	}
	projection.documents, err = c.refreshDocumentClusters(ctx, replacement)
	if err != nil {
		projection.release()

		return documentClusterProjection{}, err
	}

	return projection, nil
}

func (c *IngestConsumer) replaceDocumentClusters(
	ctx context.Context,
	docs []documentstore.Document,
) (documentClusterReplacement, error) {
	if clusters, ok := c.clusters.(contentClusterBatch); ok {
		return c.replaceDocumentClusterBatch(ctx, docs, clusters)
	}
	replacement := newDocumentClusterReplacement(docs)
	for index := range docs {
		previous, found, err := c.clusters.Lookup(ctx, documentClusterURL(docs[index]))
		if err != nil {
			return documentClusterReplacement{}, fmt.Errorf(
				"look up replaced content cluster: %w",
				err,
			)
		}
		if found {
			replacement.affectedClusterIDs[previous.ClusterID] = struct{}{}
		}
		doc, err := c.assignDocumentCluster(ctx, docs[index])
		if err != nil {
			return documentClusterReplacement{}, err
		}
		replacement.documents[index] = doc
		replacement.currentURLs[doc.NormalizedURL] = struct{}{}
		replacement.affectedClusterIDs[doc.ClusterID] = struct{}{}
		replacement.assignedClusterIDs[doc.ClusterID] = struct{}{}
	}

	return replacement, nil
}

func newDocumentClusterReplacement(
	docs []documentstore.Document,
) documentClusterReplacement {
	return documentClusterReplacement{
		documents:          docs,
		currentURLs:        make(map[string]struct{}, len(docs)),
		affectedClusterIDs: make(map[string]struct{}, len(docs)),
		assignedClusterIDs: make(map[string]struct{}, len(docs)),
	}
}

func (c *IngestConsumer) refreshDocumentClusters(
	ctx context.Context,
	replacement documentClusterReplacement,
) ([]documentstore.Document, error) {
	orderedClusterIDs := make([]string, 0, len(replacement.affectedClusterIDs))
	for clusterID := range replacement.affectedClusterIDs {
		orderedClusterIDs = append(orderedClusterIDs, clusterID)
	}
	sort.Strings(orderedClusterIDs)
	storedUpdates := make([]documentstore.Document, 0)
	for _, clusterID := range orderedClusterIDs {
		cluster, found, err := c.clusters.Cluster(ctx, clusterID)
		if err != nil {
			return nil, fmt.Errorf("read assigned content cluster: %w", err)
		}
		if !found {
			if _, assigned := replacement.assignedClusterIDs[clusterID]; assigned {
				return nil, fmt.Errorf("assigned content cluster %q is missing", clusterID)
			}

			continue
		}
		for index := range replacement.documents {
			if replacement.documents[index].ClusterID == cluster.ID {
				replacement.documents[index].RepresentativeURL = cluster.RepresentativeURL
			}
		}
		updates, err := c.storedClusterProjection(
			ctx,
			cluster,
			replacement.currentURLs,
			replacement.replay,
		)
		if err != nil {
			return nil, err
		}
		storedUpdates = append(storedUpdates, updates...)
	}

	return append(storedUpdates, replacement.documents...), nil
}

func (c *IngestConsumer) assignDocumentCluster(
	ctx context.Context,
	doc documentstore.Document,
) (documentstore.Document, error) {
	assignment, err := c.clusters.Replace(ctx, documentClusterEvidence(doc))
	if err != nil {
		return documentstore.Document{}, fmt.Errorf("replace content cluster: %w", err)
	}

	return assignedDocumentCluster(doc, assignment), nil
}

func documentClusterEvidence(doc documentstore.Document) contentcluster.Evidence {
	url := documentClusterURL(doc)
	doc.NormalizedURL = url

	return contentcluster.Evidence{
		URL:                url,
		Text:               documentClusterText(doc),
		ContentHash:        documentClusterHash(doc, url),
		CanonicalPreferred: doc.CanonicalURL != "" && doc.CanonicalURL == url,
		Quality:            documentClusterQuality(doc),
		InboundAuthority:   documentInboundAuthority(doc),
	}
}

func assignedDocumentCluster(
	doc documentstore.Document,
	assignment contentcluster.Assignment,
) documentstore.Document {
	url := documentClusterURL(doc)
	doc.NormalizedURL = url
	if doc.CanonicalURL == "" {
		doc.CanonicalURL = url
	}
	doc.ContentHash = documentClusterHash(doc, url)
	doc.ClusterID = assignment.ClusterID
	doc.RepresentativeURL = assignment.RepresentativeURL

	return doc
}

func (c *IngestConsumer) storedClusterUpdates(
	ctx context.Context,
	cluster contentcluster.Cluster,
	excluded map[string]struct{},
) ([]documentstore.Document, error) {
	return c.storedClusterProjection(ctx, cluster, excluded, false)
}

func (c *IngestConsumer) storedClusterProjection(
	ctx context.Context,
	cluster contentcluster.Cluster,
	excluded map[string]struct{},
	includeUnchanged bool,
) ([]documentstore.Document, error) {
	directory, ok := c.documents.(documentstore.DocumentDirectory)
	if !ok {
		return nil, nil
	}
	updates := make([]documentstore.Document, 0, len(cluster.MemberURLs))
	for _, memberURL := range cluster.MemberURLs {
		if _, skip := excluded[memberURL]; skip {
			continue
		}
		doc, found, err := directory.Document(ctx, memberURL)
		if err != nil {
			return nil, fmt.Errorf("read clustered document: %w", err)
		}
		if !found || !includeUnchanged && doc.ClusterID == cluster.ID &&
			doc.RepresentativeURL == cluster.RepresentativeURL {
			continue
		}
		doc.ClusterID = cluster.ID
		doc.RepresentativeURL = cluster.RepresentativeURL
		updates = append(updates, doc)
	}

	return updates, nil
}

func (c *IngestConsumer) deleteDocumentCluster(ctx context.Context, url string) error {
	if c.clusters == nil {
		return nil
	}
	if transitions, ok := c.clusters.(contentClusterTransitionDeleter); ok {
		return c.deleteDocumentClusterTransition(ctx, url, transitions)
	}
	assignment, found, err := c.clusters.Lookup(ctx, url)
	if err != nil {
		return fmt.Errorf("look up removed content cluster: %w", err)
	}
	if _, err := c.clusters.Delete(ctx, url); err != nil {
		return fmt.Errorf("delete removed content cluster: %w", err)
	}
	if !found {
		return nil
	}
	cluster, found, err := c.clusters.Cluster(ctx, assignment.ClusterID)
	if err != nil {
		return fmt.Errorf("read surviving content cluster: %w", err)
	}
	if !found {
		return nil
	}
	updates, err := c.storedClusterUpdates(ctx, cluster, nil)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	receipt, err := c.documents.Receive(ctx, updates)
	if err != nil {
		return fmt.Errorf("store surviving content cluster: %w", err)
	}
	if receipt.Busy {
		return fmt.Errorf("store surviving content cluster at capacity")
	}
	if c.index != nil {
		if err := c.indexDocuments(ctx, updates); err != nil {
			return fmt.Errorf("index surviving content cluster: %w", err)
		}
	}

	return nil
}

func documentClusterURL(doc documentstore.Document) string {
	if doc.NormalizedURL != "" {
		return strings.TrimSpace(doc.NormalizedURL)
	}

	return strings.TrimSpace(doc.CanonicalURL)
}

func documentClusterText(doc documentstore.Document) string {
	parts := make([]string, 0, len(doc.Headings)+2)
	parts = append(parts, doc.Title)
	parts = append(parts, doc.Headings...)
	parts = append(parts, doc.ExtractedText)

	return strings.Join(parts, " ")
}

func documentClusterHash(doc documentstore.Document, url string) string {
	if contentHash := strings.TrimSpace(doc.ContentHash); contentHash != "" {
		return contentHash
	}
	text := strings.TrimSpace(documentClusterText(doc))
	if text == "" {
		text = url
	}
	digest := sha256.Sum256([]byte(text))

	return hex.EncodeToString(digest[:])
}

func documentClusterQuality(doc documentstore.Document) float64 {
	if !doc.ContentQuality.Known {
		return 0
	}

	return doc.ContentQuality.Score
}

func documentInboundAuthority(doc documentstore.Document) float64 {
	sources := make(map[string]struct{}, len(doc.Inlinks))
	for _, anchor := range doc.Inlinks {
		if anchor.NoFollow || anchor.UserGenerated || anchor.Sponsored {
			continue
		}
		sourceURL := strings.TrimSpace(anchor.URL)
		if sourceURL != "" {
			sources[sourceURL] = struct{}{}
		}
	}

	return float64(len(sources))
}
