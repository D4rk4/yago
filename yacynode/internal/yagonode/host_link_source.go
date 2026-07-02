package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/hostlinks"
)

const (
	hostLinkGraphScanFailedMessage = "host link graph scan failed"
	hostLinkMaxLinkedHosts         = 10000
	hostLinkMaxReferencesPerHost   = 200
	secondsPerDay                  = 86400
)

type storedDocumentHostLinks struct {
	documents documentstore.StoredDocuments
}

type hostLinkReference struct {
	ModifiedDay int64
	Count       int
}

func (s storedDocumentHostLinks) IncomingHostLinks(ctx context.Context) hostlinks.Graph {
	graph := hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition}
	if s.documents == nil {
		return graph
	}

	incoming := map[string]map[string]hostLinkReference{}
	err := s.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		collectDocumentHostLinks(incoming, doc)

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, hostLinkGraphScanFailedMessage, slog.Any("error", err))

		return graph
	}

	graph.LinkedHosts = hostLinkGraphHosts(incoming)

	return graph
}

func collectDocumentHostLinks(
	incoming map[string]map[string]hostLinkReference,
	doc documentstore.Document,
) {
	source, ok := documentHostHash(doc.NormalizedURL)
	if !ok {
		return
	}
	day := documentModifiedDay(doc)

	for _, outlink := range doc.Outlinks {
		target, ok := documentHostHash(outlink)
		if !ok || target == source {
			continue
		}

		references := incoming[target]
		if references == nil {
			references = map[string]hostLinkReference{}
			incoming[target] = references
		}

		reference := references[source]
		reference.Count++
		reference.ModifiedDay = max(reference.ModifiedDay, day)
		references[source] = reference
	}
}

func documentHostHash(rawURL string) (string, bool) {
	urlHash, hashErr := yacymodel.HashURL(rawURL)
	hostHash, hostErr := urlHash.HostHash()

	return hostHash, strings.TrimSpace(rawURL) != "" && hashErr == nil && hostErr == nil
}

func documentModifiedDay(doc documentstore.Document) int64 {
	moment := doc.FetchedAt
	if moment.IsZero() {
		moment = doc.IndexedAt
	}
	if moment.IsZero() {
		return 0
	}

	return moment.UTC().Truncate(24*time.Hour).Unix() / secondsPerDay
}

func hostLinkGraphHosts(
	incoming map[string]map[string]hostLinkReference,
) []hostlinks.LinkedHost {
	targets := firstSortedKeys(incoming, hostLinkMaxLinkedHosts)
	hosts := make([]hostlinks.LinkedHost, 0, len(targets))
	for _, target := range targets {
		hosts = append(hosts, hostlinks.LinkedHost{
			HostHash:   target,
			References: hostLinkReferenceMessages(incoming[target]),
		})
	}

	return hosts
}

func hostLinkReferenceMessages(
	references map[string]hostLinkReference,
) []json.RawMessage {
	sources := firstSortedKeys(references, hostLinkMaxReferencesPerHost)
	messages := make([]json.RawMessage, 0, len(sources))
	for _, source := range sources {
		reference := references[source]
		messages = append(messages, json.RawMessage(fmt.Sprintf(
			`{"h":%q,"m":%q,"c":%q}`,
			source,
			strconv.FormatInt(reference.ModifiedDay, 10),
			strconv.Itoa(reference.Count),
		)))
	}

	return messages
}

func firstSortedKeys[V any](values map[string]V, limit int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) <= limit {
		return keys
	}

	return keys[:limit]
}
