package hostlinkgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateSnapshotAcceptsBoundaries(t *testing.T) {
	exactReferenceLimit := json.RawMessage(
		`"` + strings.Repeat("a", MaximumSnapshotReferenceBytes-2) + `"`,
	)
	tests := []struct {
		name  string
		graph Graph
	}{
		{
			name:  "empty graph",
			graph: Graph{RowDefinition: HostReferenceRowDefinition},
		},
		{
			name: "reference byte limit",
			graph: Graph{
				RowDefinition: HostReferenceRowDefinition,
				LinkedHosts: []LinkedHost{
					{HostHash: "target", References: []json.RawMessage{exactReferenceLimit}},
				},
			},
		},
		{
			name:  "linked host limit",
			graph: validationSnapshot(MaximumSnapshotLinkedHosts, 1),
		},
		{
			name: "reference limits",
			graph: validationSnapshot(
				MaximumSnapshotReferences/MaximumSnapshotReferencesPerHost,
				MaximumSnapshotReferencesPerHost,
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateSnapshot(test.graph); err != nil {
				t.Fatalf("ValidateSnapshot() error = %v", err)
			}
		})
	}
}

func TestValidateSnapshotRejectsInvalidGraph(t *testing.T) {
	for _, test := range invalidSnapshotCases() {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateSnapshot(test.graph); !errors.Is(err, test.want) {
				t.Fatalf("ValidateSnapshot() error = %v, want %v", err, test.want)
			}
		})
	}
}

type invalidSnapshotCase struct {
	name  string
	graph Graph
	want  error
}

func invalidSnapshotCases() []invalidSnapshotCase {
	return append(invalidSnapshotStructureCases(), invalidSnapshotReferenceCases()...)
}

func invalidSnapshotStructureCases() []invalidSnapshotCase {
	return []invalidSnapshotCase{
		{
			name:  "row definition",
			graph: Graph{},
			want:  errSnapshotRowDefinition,
		},
		{
			name: "linked hosts",
			graph: Graph{
				RowDefinition: HostReferenceRowDefinition,
				LinkedHosts:   make([]LinkedHost, MaximumSnapshotLinkedHosts+1),
			},
			want: errSnapshotLinkedHosts,
		},
		{
			name: "host hash",
			graph: Graph{
				RowDefinition: HostReferenceRowDefinition,
				LinkedHosts: []LinkedHost{
					{HostHash: "short", References: []json.RawMessage{json.RawMessage(`{}`)}},
				},
			},
			want: errSnapshotHostHash,
		},
		{
			name: "duplicate host",
			graph: Graph{
				RowDefinition: HostReferenceRowDefinition,
				LinkedHosts: []LinkedHost{
					{HostHash: "target", References: []json.RawMessage{json.RawMessage(`{}`)}},
					{HostHash: "target", References: []json.RawMessage{json.RawMessage(`{}`)}},
				},
			},
			want: errSnapshotDuplicateHost,
		},
		{
			name: "no host references",
			graph: Graph{
				RowDefinition: HostReferenceRowDefinition,
				LinkedHosts:   []LinkedHost{{HostHash: "target"}},
			},
			want: errSnapshotHostReferences,
		},
		{
			name:  "host reference limit",
			graph: validationSnapshot(1, MaximumSnapshotReferencesPerHost+1),
			want:  errSnapshotHostReferences,
		},
		{
			name:  "total reference limit",
			graph: snapshotAboveTotalReferenceLimit(),
			want:  errSnapshotReferences,
		},
	}
}

func invalidSnapshotReferenceCases() []invalidSnapshotCase {
	return []invalidSnapshotCase{
		{
			name:  "empty reference",
			graph: snapshotWithReference(nil),
			want:  errSnapshotReferenceEmpty,
		},
		{
			name: "reference byte limit",
			graph: snapshotWithReference(json.RawMessage(
				`"` + strings.Repeat("a", MaximumSnapshotReferenceBytes-1) + `"`,
			)),
			want: errSnapshotReferenceTooLarge,
		},
		{
			name:  "reference JSON",
			graph: snapshotWithReference(json.RawMessage(`{`)),
			want:  errSnapshotReferenceInvalidJSON,
		},
	}
}

func validationSnapshot(linkedHosts int, referencesPerHost int) Graph {
	graph := Graph{
		RowDefinition: HostReferenceRowDefinition,
		LinkedHosts:   make([]LinkedHost, linkedHosts),
	}
	for hostIndex := range linkedHosts {
		references := make([]json.RawMessage, referencesPerHost)
		for referenceIndex := range referencesPerHost {
			references[referenceIndex] = json.RawMessage(`{}`)
		}
		graph.LinkedHosts[hostIndex] = LinkedHost{
			HostHash:   fmt.Sprintf("%06x", hostIndex),
			References: references,
		}
	}

	return graph
}

func snapshotAboveTotalReferenceLimit() Graph {
	graph := validationSnapshot(
		MaximumSnapshotReferences/MaximumSnapshotReferencesPerHost,
		MaximumSnapshotReferencesPerHost,
	)
	graph.LinkedHosts = append(graph.LinkedHosts, LinkedHost{
		HostHash:   fmt.Sprintf("%06x", len(graph.LinkedHosts)),
		References: []json.RawMessage{json.RawMessage(`{}`)},
	})

	return graph
}

func snapshotWithReference(reference json.RawMessage) Graph {
	return Graph{
		RowDefinition: HostReferenceRowDefinition,
		LinkedHosts: []LinkedHost{
			{HostHash: "target", References: []json.RawMessage{reference}},
		},
	}
}
