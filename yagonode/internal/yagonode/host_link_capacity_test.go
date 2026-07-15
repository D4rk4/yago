package yagonode

import "testing"

func TestRecordHostLinkBoundsRetainedGraph(t *testing.T) {
	accumulator := hostLinkAccumulator{
		incoming: map[string]map[string]hostLinkReference{},
	}
	capacity := hostLinkCapacity{linkedHosts: 2, referencesPerHost: 2, references: 3}
	recordHostLink(&accumulator, "target-a", "source-a", 1, capacity)
	recordHostLink(&accumulator, "target-a", "source-a", 2, capacity)
	recordHostLink(&accumulator, "target-a", "source-b", 1, capacity)
	recordHostLink(&accumulator, "target-a", "source-c", 3, capacity)
	recordHostLink(&accumulator, "target-b", "source-a", 1, capacity)
	recordHostLink(&accumulator, "target-b", "source-b", 1, capacity)
	recordHostLink(&accumulator, "target-c", "source-a", 1, capacity)
	incoming := accumulator.incoming

	if len(incoming) != 2 || len(incoming["target-a"]) != 2 {
		t.Fatalf("retained graph = %#v", incoming)
	}
	if got := incoming["target-a"]["source-a"]; got.Count != 2 || got.ModifiedDay != 2 {
		t.Fatalf("retained reference = %#v", got)
	}
	if _, found := incoming["target-a"]["source-c"]; found {
		t.Fatal("source beyond the per-target cap was retained")
	}
	if _, found := incoming["target-c"]; found {
		t.Fatal("target beyond the graph cap was retained")
	}
	if accumulator.references != capacity.references ||
		len(incoming["target-b"]) != 1 {
		t.Fatalf("total retained references = %d/%#v", accumulator.references, incoming)
	}
}

func TestRecordHostLinkBoundsLinkedHostsBeforeReferences(t *testing.T) {
	accumulator := hostLinkAccumulator{
		incoming: map[string]map[string]hostLinkReference{},
	}
	capacity := hostLinkCapacity{linkedHosts: 1, referencesPerHost: 2, references: 3}
	recordHostLink(&accumulator, "target-a", "source-a", 1, capacity)
	recordHostLink(&accumulator, "target-b", "source-b", 1, capacity)

	if len(accumulator.incoming) != 1 || accumulator.references != 1 {
		t.Fatalf(
			"retained graph = %#v, references = %d",
			accumulator.incoming,
			accumulator.references,
		)
	}
	if _, found := accumulator.incoming["target-b"]; found {
		t.Fatal("target beyond the linked-host cap was retained")
	}
}
