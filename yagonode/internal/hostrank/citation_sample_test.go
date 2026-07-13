package hostrank

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestCitationSampleIsBoundedDeduplicatedAndOrderIndependent(t *testing.T) {
	citations := []Citation{
		{SourceURL: "https://a.example/page", TargetURL: "https://one.test/a", Confidence: 0.3},
		{SourceURL: "https://a.example/page", TargetURL: "https://one.test/b", Confidence: 1},
		{SourceURL: "https://b.example/page", TargetURL: "https://two.test/", Confidence: 0.5},
		{SourceURL: "https://c.example/page", TargetURL: "https://three.test/", Confidence: 1},
		{SourceURL: "https://d.example/page", TargetURL: "https://four.test/", Confidence: 0.4},
	}
	forward := newCitationSample(2)
	forward.Add(citations...)
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

func TestCitationSampleCollapsesTargetPagesWithoutOrderDependence(t *testing.T) {
	citations := []Citation{
		{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.test/z",
			Confidence: 0.3,
		},
		{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.test/a",
			Confidence: 0.8,
		},
		{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.test/m",
			Confidence: 0.6,
		},
	}
	forward := newCitationSample(1)
	forward.Add(citations...)
	reverse := newCitationSample(1)
	for index := len(citations) - 1; index >= 0; index-- {
		reverse.Add(citations[index])
	}
	want := []Citation{
		{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.test/",
			Confidence: 0.8,
		},
	}
	if !reflect.DeepEqual(forward.Citations(), want) ||
		!reflect.DeepEqual(reverse.Citations(), want) {
		t.Fatalf("collapsed target pages = %#v/%#v", forward.Citations(), reverse.Citations())
	}
}

func TestCitationSamplePreservesIPDomainEdgesAcrossResampling(t *testing.T) {
	citations := []Citation{
		{
			SourceURL: "https://192.0.2.1/source", TargetURL: "https://[2001:db8::2]/target",
			Confidence: 1,
		},
		{
			SourceURL: "https://[2001:db8::3]/source", TargetURL: "https://198.51.100.2/target",
			Confidence: 0.7,
		},
	}
	forward := NewCitationSample()
	forward.Add(citations...)
	reverse := NewCitationSample()
	reverse.Add(citations[1], citations[0])
	if !reflect.DeepEqual(forward.Citations(), reverse.Citations()) {
		t.Fatalf("IP samples = %#v/%#v", forward.Citations(), reverse.Citations())
	}
	table, err := ComputeDomainAuthority(t.Context(), forward.Citations(), DomainOptions{})
	if err != nil || len(table) != 4 || table.Rank("2001:db8::2") == 0 ||
		table.Rank("198.51.100.2") == 0 {
		t.Fatalf("IP domain authority = %#v/%v", table, err)
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
		{key: "b", priority: 1, queueOffset: 0},
		{key: "a", priority: 1, queueOffset: 1},
	}
	if !queue.Less(0, 1) {
		t.Fatal("priority queue did not keep the worst tie at the root")
	}
	queue.Swap(0, 1)
	if queue[0].key != "a" || queue[0].queueOffset != 0 || queue[1].queueOffset != 1 ||
		!citationPrecedes(queue[0], queue[1]) {
		t.Fatal("citation priority tie is not deterministic")
	}
}

func TestCitationSampleHasHardCountAndRetainedByteBounds(t *testing.T) {
	sample := newCitationSample(maximumDomainCitations + 1)
	for index := range maximumDomainCitations + 128 {
		sample.Add(Citation{
			SourceURL:  fmt.Sprintf("https://source-%d.com/page", index),
			TargetURL:  fmt.Sprintf("https://target-%d.net/page", index),
			Confidence: 1,
		})
	}
	if sample.limit != maximumDomainCitations || len(sample.Citations()) != maximumDomainCitations {
		t.Fatalf("sample limits = %d/%d", sample.limit, len(sample.Citations()))
	}
	if sample.retainedBytes != len(sample.citations)*maximumCitationRetainedBytes ||
		sample.retainedBytes > maximumCitationSampleBytes || len(sample.pages) != len(sample.citations) ||
		len(sample.domainEdges) > len(sample.citations) {
		t.Fatalf("sample bounds = bytes:%d pages:%d edges:%d citations:%d",
			sample.retainedBytes, len(sample.pages), len(sample.domainEdges), len(sample.citations))
	}
}

func TestCitationSampleRejectsOversizedURLsAndOwnsCompactStorage(t *testing.T) {
	oversized := strings.Repeat("x", maximumCitationURLBytes+1)
	rejected := newCitationSample(1)
	rejected.Add(Citation{
		SourceURL: oversized, TargetURL: "https://target.example/", Confidence: 1,
	})
	rejected.Add(Citation{
		SourceURL: "https://source.example/", TargetURL: oversized, Confidence: 1,
	})
	rejected.Add(Citation{
		SourceURL: "https://source.example/", TargetURL: "https://target.example/", Confidence: 0,
	})
	rejected.Add(Citation{
		SourceURL:  "https://" + strings.Repeat("a", maximumCitationDomainBytes+1) + "/",
		TargetURL:  "https://target.example/",
		Confidence: 1,
	})
	if len(rejected.Citations()) != 0 || rejected.retainedBytes != 0 {
		t.Fatalf("invalid citations retained = %#v", rejected.Citations())
	}
	sourcePrefix := "https://source.example/"
	targetPrefix := "https://target.test/"
	source := sourcePrefix + strings.Repeat("x", maximumCitationURLBytes-len(sourcePrefix))
	target := targetPrefix + strings.Repeat("x", maximumCitationURLBytes-len(targetPrefix))
	boundary := newCitationSample(1)
	boundary.Add(Citation{SourceURL: source, TargetURL: target, Confidence: 1})
	if len(boundary.Citations()) != 1 || boundary.retainedBytes != maximumCitationRetainedBytes {
		t.Fatalf("boundary citation retained = %#v", boundary.Citations())
	}

	backing := "beforehttps://source.example/pageafterhttps://target.test/path"
	source = backing[len("before"):len("beforehttps://source.example/page")]
	target = backing[len("beforehttps://source.example/pageafter"):]
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

func TestCitationSampleRejectsOneHundredThousandInternalLinksBeforeRetention(t *testing.T) {
	sample := NewCitationSample()
	for index := range 100_000 {
		sample.Add(Citation{
			SourceURL:  fmt.Sprintf("https://page-%d.internal.example/source", index),
			TargetURL:  "https://asset.internal.example/target",
			Confidence: 1,
		})
	}
	external := []Citation{
		{SourceURL: "https://one.example/page", TargetURL: "https://first.test/", Confidence: 1},
		{SourceURL: "https://two.example/page", TargetURL: "https://second.test/", Confidence: 0.8},
		{
			SourceURL:  "https://three.example/page",
			TargetURL:  "https://third.test/",
			Confidence: 0.6,
		},
	}
	sample.Add(external...)
	if got := sample.Citations(); len(got) != len(external) {
		t.Fatalf("retained citations after internal flood = %#v", got)
	}
	for _, citation := range sample.Citations() {
		if RegistrableDomain(citation.SourceURL) == RegistrableDomain(citation.TargetURL) {
			t.Fatalf("internal citation retained = %#v", citation)
		}
	}
}

func TestCitationSampleCompactsDomainEdgesBeforeGlobalBudget(t *testing.T) {
	citations := make([]Citation, 0, 68)
	for index := range 64 {
		citations = append(citations, Citation{
			SourceURL:  fmt.Sprintf("https://bulk-source.example/page/%d", index),
			TargetURL:  fmt.Sprintf("https://bulk-target.test/out/%d", index),
			Confidence: float64(index%5+1) / 5,
		})
	}
	for index := range 4 {
		citations = append(citations, Citation{
			SourceURL:  fmt.Sprintf("https://sparse-source-%d.com/page", index),
			TargetURL:  fmt.Sprintf("https://sparse-target-%d.net/page", index),
			Confidence: 1,
		})
	}
	forward := newCitationSample(maximumSourcePagesPerDomain + 4)
	forward.Add(citations...)
	reverse := newCitationSample(maximumSourcePagesPerDomain + 4)
	for index := len(citations) - 1; index >= 0; index-- {
		reverse.Add(citations[index])
	}
	retained := forward.Citations()
	if !reflect.DeepEqual(retained, reverse.Citations()) {
		t.Fatalf("domain-compacted samples differ = %#v/%#v", retained, reverse.Citations())
	}
	for stride := 1; stride < len(citations); stride++ {
		if citationTestGreatestCommonDivisor(stride, len(citations)) != 1 {
			continue
		}
		shuffled := make([]Citation, len(citations))
		for index := range shuffled {
			shuffled[index] = citations[(stride+index*stride)%len(citations)]
		}
		sample := newCitationSample(maximumSourcePagesPerDomain + 4)
		sample.Add(shuffled...)
		if !reflect.DeepEqual(retained, sample.Citations()) {
			t.Fatalf("stride %d changed sample = %#v", stride, sample.Citations())
		}
	}
	bulkPages := 0
	sparseTargets := make(map[string]struct{})
	for _, citation := range retained {
		if RegistrableDomain(citation.SourceURL) == "bulk-source.example" {
			bulkPages++
		} else {
			sparseTargets[RegistrableDomain(citation.TargetURL)] = struct{}{}
		}
	}
	if len(retained) != maximumSourcePagesPerDomain+4 ||
		bulkPages != maximumSourcePagesPerDomain || len(sparseTargets) != 4 {
		t.Fatalf("domain-compacted sample = total:%d bulk:%d sparse:%d %#v",
			len(retained), bulkPages, len(sparseTargets), retained)
	}
}

func citationTestGreatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}

	return left
}
