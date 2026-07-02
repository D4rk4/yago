package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/hostlinks"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

const (
	hostLinkGraphScanFailedMessage = "host link graph scan failed"
	hostLinkMaxLinkedHosts         = 10000
	hostLinkMaxReferencesPerHost   = 200
	hostLinkShortDayLayout         = "20060102"
	hostReferenceHostHashLength    = 6
	secondsPerDay                  = 86400
)

type storedURLHostLinks struct {
	rows urlmeta.StoredURLMetadataRows
}

type hostLinkReference struct {
	ModifiedDay int64
	Count       int
}

func (s storedURLHostLinks) IncomingHostLinks(ctx context.Context) hostlinks.Graph {
	graph := hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition}
	if s.rows == nil {
		return graph
	}

	incoming := map[string]map[string]hostLinkReference{}
	err := s.rows.StoredURLMetadataRows(ctx, func(row yacymodel.URIMetadataRow) (bool, error) {
		target, source, ok := hostLinkEdge(row)
		if !ok {
			return true, nil
		}

		references := incoming[target]
		if references == nil {
			references = map[string]hostLinkReference{}
			incoming[target] = references
		}

		reference := references[source]
		reference.Count++
		reference.ModifiedDay = max(reference.ModifiedDay, hostLinkModifiedDay(row))
		references[source] = reference

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, hostLinkGraphScanFailedMessage, slog.Any("error", err))

		return graph
	}

	graph.LinkedHosts = hostLinkGraphHosts(incoming)

	return graph
}

func hostLinkEdge(row yacymodel.URIMetadataRow) (string, string, bool) {
	targetURLHash, err := row.URLHash()
	if err != nil {
		return "", "", false
	}

	referrerURLHash, err := yacymodel.ParseURLHash(row.Properties[yacymodel.URLMetaReferrer])
	if err != nil {
		return "", "", false
	}

	targetHostHash := string(targetURLHash)[yacymodel.HashLength-hostReferenceHostHashLength:]
	sourceHostHash := string(referrerURLHash)[yacymodel.HashLength-hostReferenceHostHashLength:]

	if targetHostHash == sourceHostHash {
		return "", "", false
	}

	return targetHostHash, sourceHostHash, true
}

func hostLinkModifiedDay(row yacymodel.URIMetadataRow) int64 {
	freshness := row.Freshness()
	if len(freshness) < len(hostLinkShortDayLayout) {
		return 0
	}

	day, err := time.ParseInLocation(
		hostLinkShortDayLayout,
		freshness[:len(hostLinkShortDayLayout)],
		time.UTC,
	)
	if err != nil {
		return 0
	}

	return day.Unix() / secondsPerDay
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
