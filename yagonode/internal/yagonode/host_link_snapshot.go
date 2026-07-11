package yagonode

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
)

const hostLinkSnapshotTTL = 5 * time.Minute

type hostLinkGraphScan func(context.Context) (hostlinks.Graph, error)

type cachedStoredDocumentHostLinks struct {
	scan    hostLinkGraphScan
	now     func() time.Time
	mu      sync.Mutex
	graph   hostlinks.Graph
	expires time.Time
}

func newCachedStoredDocumentHostLinks(
	documents storedDocumentHostLinks,
) *cachedStoredDocumentHostLinks {
	return &cachedStoredDocumentHostLinks{scan: documents.scan, now: time.Now}
}

func (c *cachedStoredDocumentHostLinks) IncomingHostLinks(ctx context.Context) hostlinks.Graph {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if now.Before(c.expires) {
		return c.graph
	}
	graph, err := c.scan(ctx)
	if err != nil {
		slog.WarnContext(ctx, hostLinkGraphScanFailedMessage, slog.Any("error", err))

		return hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition}
	}
	c.graph = graph
	c.expires = now.Add(hostLinkSnapshotTTL)

	return graph
}
