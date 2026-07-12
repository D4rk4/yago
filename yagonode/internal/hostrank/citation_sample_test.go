package hostrank

import (
	"fmt"
	"reflect"
	"strings"
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

func TestCitationSampleHasHardCountAndRetainedByteBounds(t *testing.T) {
	sample := newCitationSample(maximumDomainCitations + 1)
	for index := range maximumDomainCitations + 128 {
		sample.Add(Citation{
			SourceURL:  fmt.Sprintf("https://source.example/%d", index),
			TargetURL:  fmt.Sprintf("https://target.example/%d", index),
			Confidence: 1,
		})
	}
	if sample.limit != maximumDomainCitations || len(sample.Citations()) != maximumDomainCitations {
		t.Fatalf("sample limits = %d/%d", sample.limit, len(sample.Citations()))
	}
	if sample.retainedBytes != len(sample.citations)*maximumCitationRetainedBytes ||
		sample.retainedBytes > maximumCitationSampleBytes {
		t.Fatalf("sample retained bytes = %d", sample.retainedBytes)
	}
}

func TestCitationSampleRejectsOversizedURLsAndOwnsCompactStorage(t *testing.T) {
	oversized := strings.Repeat("x", maximumCitationURLBytes+1)
	rejected := newCitationSample(1)
	rejected.Add(Citation{SourceURL: oversized, TargetURL: "target", Confidence: 1})
	rejected.Add(Citation{SourceURL: "source", TargetURL: oversized, Confidence: 1})
	if len(rejected.Citations()) != 0 || rejected.retainedBytes != 0 {
		t.Fatalf("oversized citations retained = %#v", rejected.Citations())
	}
	boundary := newCitationSample(1)
	exact := strings.Repeat("x", maximumCitationURLBytes)
	boundary.Add(Citation{SourceURL: exact, TargetURL: exact, Confidence: 1})
	if len(boundary.Citations()) != 1 || boundary.retainedBytes != maximumCitationRetainedBytes {
		t.Fatalf("boundary citation retained = %#v", boundary.Citations())
	}

	backing := strings.Repeat("x", 1<<20)
	source := backing[100:120]
	target := backing[200:225]
	sample := newCitationSample(1)
	sample.Add(Citation{SourceURL: source, TargetURL: target, Confidence: 1})
	retained := sample.citations[0]
	backingStart := uintptr(reflect.ValueOf(backing).UnsafePointer())
	backingEnd := backingStart + uintptr(len(backing))
	sourceStart := uintptr(reflect.ValueOf(retained.citation.SourceURL).UnsafePointer())
	targetStart := uintptr(reflect.ValueOf(retained.citation.TargetURL).UnsafePointer())
	keyStart := uintptr(reflect.ValueOf(retained.key).UnsafePointer())
	if sourceStart >= backingStart && sourceStart < backingEnd ||
		targetStart >= backingStart && targetStart < backingEnd {
		t.Fatal("citation sample retained source storage")
	}
	if sourceStart != keyStart || targetStart != keyStart+uintptr(len(source)+1) {
		t.Fatal("citation URLs do not share the compact identity storage")
	}
}
