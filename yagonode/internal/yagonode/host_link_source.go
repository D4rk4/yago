package yagonode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
)

const (
	secondsPerDay = 86400
)

type hostLinkReference struct {
	ModifiedDay int64
	Count       int
}

type hostLinkCapacity struct {
	linkedHosts       int
	referencesPerHost int
	references        int
}

type hostLinkAccumulator struct {
	incoming   map[string]map[string]hostLinkReference
	references int
}

func collectDocumentHostLinks(
	accumulator *hostLinkAccumulator,
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

		recordHostLink(
			accumulator,
			target,
			source,
			day,
			hostLinkCapacity{
				linkedHosts:       hostlinks.MaximumSnapshotLinkedHosts,
				referencesPerHost: hostlinks.MaximumSnapshotReferencesPerHost,
				references:        hostlinks.MaximumSnapshotReferences,
			},
		)
	}
}

func recordHostLink(
	accumulator *hostLinkAccumulator,
	target string,
	source string,
	modifiedDay int64,
	capacity hostLinkCapacity,
) {
	references := accumulator.incoming[target]
	_, referenceFound := references[source]
	if !referenceFound && accumulator.references >= capacity.references {
		return
	}
	if references == nil {
		if len(accumulator.incoming) >= capacity.linkedHosts {
			return
		}
		references = map[string]hostLinkReference{}
		accumulator.incoming[target] = references
	}
	if !referenceFound &&
		len(references) >= capacity.referencesPerHost {
		return
	}
	if !referenceFound {
		accumulator.references++
	}
	reference := references[source]
	reference.Count++
	reference.ModifiedDay = max(reference.ModifiedDay, modifiedDay)
	references[source] = reference
}

func documentHostHash(rawURL string) (string, bool) {
	urlHash, hashErr := yagomodel.HashURL(rawURL)
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
	targets := firstSortedKeys(incoming, hostlinks.MaximumSnapshotLinkedHosts)
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
	sources := firstSortedKeys(references, hostlinks.MaximumSnapshotReferencesPerHost)
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
