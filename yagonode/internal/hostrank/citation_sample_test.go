package hostrank

import (
	"reflect"
	"testing"
)

func TestCitationSampleIsBoundedDeduplicatedAndOrderIndependent(t *testing.T) {
	citations := []Citation{
		{SourceURL: "https://a/", TargetURL: "https://one/", Confidence: 1},
		{SourceURL: "https://b/", TargetURL: "https://two/", Confidence: 0.5},
		{SourceURL: "https://c/", TargetURL: "https://three/", Confidence: 1},
		{SourceURL: "https://d/", TargetURL: "https://four/", Confidence: 0.4},
	}
	forward := newCitationSample(2)
	forward.Add(citations...)
	retained := forward.Citations()
	forward.Add(retained[0])
	reverse := newCitationSample(2)
	for index := len(citations) - 1; index >= 0; index-- {
		reverse.Add(citations[index])
	}
	if got, want := forward.Citations(), reverse.Citations(); len(got) != 2 ||
		!reflect.DeepEqual(got, want) {
		t.Fatalf("samples = %#v, %#v", got, want)
	}
	copy := forward.Citations()
	copy[0].SourceURL = "changed"
	if reflect.DeepEqual(copy, forward.Citations()) {
		t.Fatal("citation sample exposed mutable state")
	}
}

func TestCitationSampleHandlesEmptyAndPriorityTies(t *testing.T) {
	if NewCitationSample() == nil || newCitationSample(0).Citations() == nil {
		t.Fatal("citation sample construction failed")
	}
	var missing *CitationSample
	missing.Add(Citation{})
	if missing.Citations() != nil {
		t.Fatal("nil citation sample returned values")
	}
	queue := citationPriorityQueue{
		{key: "b", priority: 1},
		{key: "a", priority: 1},
	}
	if !queue.Less(0, 1) {
		t.Fatal("priority queue did not keep the worst tie at the root")
	}
	queue.Swap(0, 1)
	if queue[0].key != "a" || !citationPrecedes(queue[0], queue[1]) {
		t.Fatal("citation priority tie is not deterministic")
	}
}
