package hostlinks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestSnapshotHolderServesCompatibleEmptyGraphBeforeReplacement(t *testing.T) {
	for _, holder := range []*SnapshotHolder{{}, NewSnapshotHolder()} {
		graph := holder.IncomingHostLinks(t.Context())

		if graph.RowDefinition != HostReferenceRowDefinition {
			t.Fatalf("row definition = %q", graph.RowDefinition)
		}
		if len(graph.LinkedHosts) != 0 {
			t.Fatalf("linked hosts = %v, want empty", graph.LinkedHosts)
		}
	}
}

func TestSnapshotHolderCopiesReplacementInput(t *testing.T) {
	holder := NewSnapshotHolder()
	input := snapshotHolderGraph("first")

	holder.Replace(input)
	input.RowDefinition = "changed"
	input.LinkedHosts[0].HostHash = "changed"
	input.LinkedHosts[0].References[0][2] = 'x'
	input.LinkedHosts[0].References = append(
		input.LinkedHosts[0].References,
		json.RawMessage(`{"unexpected":true}`),
	)

	assertSnapshotHolderGraph(t, holder.IncomingHostLinks(t.Context()), "first")
}

func TestSnapshotHolderReturnsDefensiveCopies(t *testing.T) {
	holder := NewSnapshotHolder()
	holder.Replace(snapshotHolderGraph("first"))

	first := holder.IncomingHostLinks(t.Context())
	first.RowDefinition = "changed"
	first.LinkedHosts[0].HostHash = "changed"
	first.LinkedHosts[0].References[0][2] = 'x'
	first.LinkedHosts[0].References = append(
		first.LinkedHosts[0].References,
		json.RawMessage(`{"unexpected":true}`),
	)
	first.LinkedHosts = append(first.LinkedHosts, LinkedHost{HostHash: "unexpected"})

	assertSnapshotHolderGraph(t, holder.IncomingHostLinks(t.Context()), "first")
}

func TestSnapshotHolderAtomicallyReplacesConcurrentSnapshots(t *testing.T) {
	holder := NewSnapshotHolder()
	holder.Replace(snapshotHolderGraph("first"))

	const replacements = 200
	start := make(chan struct{})
	failures := make(chan string, 8)
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range replacements {
				graph := holder.IncomingHostLinks(context.Background())
				if issue := snapshotHolderGraphIssue(graph); issue != "" {
					failures <- issue
					return
				}
			}
		}()
	}

	close(start)
	for replacement := range replacements {
		if replacement%2 == 0 {
			holder.Replace(snapshotHolderGraph("second"))
			continue
		}
		holder.Replace(snapshotHolderGraph("first"))
	}
	readers.Wait()
	close(failures)
	for issue := range failures {
		t.Error(issue)
	}

	assertSnapshotHolderGraph(t, holder.IncomingHostLinks(t.Context()), "first")
}

func snapshotHolderGraph(identity string) Graph {
	return Graph{
		RowDefinition: identity,
		LinkedHosts: []LinkedHost{
			{
				HostHash: identity,
				References: []json.RawMessage{
					json.RawMessage(`{"identity":"` + identity + `"}`),
					nil,
				},
			},
		},
	}
}

func snapshotHolderGraphIssue(graph Graph) string {
	switch graph.RowDefinition {
	case "first", "second":
		identity := graph.RowDefinition
		if len(graph.LinkedHosts) != 1 ||
			graph.LinkedHosts[0].HostHash != identity ||
			len(graph.LinkedHosts[0].References) != 2 ||
			string(graph.LinkedHosts[0].References[0]) != `{"identity":"`+identity+`"}` ||
			graph.LinkedHosts[0].References[1] != nil {
			return "reader observed a partial published snapshot"
		}
		return ""
	default:
		return "reader observed an unpublished snapshot"
	}
}

func assertSnapshotHolderGraph(t *testing.T, graph Graph, identity string) {
	t.Helper()

	if graph.RowDefinition != identity {
		t.Fatalf("row definition = %q, want %q", graph.RowDefinition, identity)
	}
	if len(graph.LinkedHosts) != 1 {
		t.Fatalf("linked hosts = %v, want one", graph.LinkedHosts)
	}
	linkedHost := graph.LinkedHosts[0]
	if linkedHost.HostHash != identity {
		t.Fatalf("host hash = %q, want %q", linkedHost.HostHash, identity)
	}
	if len(linkedHost.References) != 2 {
		t.Fatalf("references = %q, want two", linkedHost.References)
	}
	wantReference := `{"identity":"` + identity + `"}`
	if string(linkedHost.References[0]) != wantReference {
		t.Fatalf("reference = %q, want %q", linkedHost.References[0], wantReference)
	}
	if linkedHost.References[1] != nil {
		t.Fatalf("nil reference = %q, want nil", linkedHost.References[1])
	}
}
