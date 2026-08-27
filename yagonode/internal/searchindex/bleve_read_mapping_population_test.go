package searchindex

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

type bleveReadMappingContextProbe struct {
	context.Context
	errorsBeforeCancellation int
	calls                    int
}

func (p *bleveReadMappingContextProbe) Err() error {
	p.calls++
	if p.calls > p.errorsBeforeCancellation {
		return context.Canceled
	}

	return nil
}

type bleveReadCachePathOnlySegmentProbe struct {
	path string
}

func (p bleveReadCachePathOnlySegmentProbe) Path() string {
	return p.path
}

func TestPopulateBleveReadMappingTouchesEveryPageBoundary(t *testing.T) {
	mapping := []byte{1, 10, 11, 12, 2, 20, 21, 22, 3, 30}
	want := append([]byte(nil), mapping...)
	baseline, err := populateBleveReadMapping(t.Context(), mapping, 4)
	if err != nil || baseline.pages != 3 || !slices.Equal(mapping, want) {
		t.Fatalf("baseline=%#v error=%v", baseline, err)
	}
	nonboundary := append([]byte(nil), mapping...)
	nonboundary[5]++
	nonboundaryReport, err := populateBleveReadMapping(t.Context(), nonboundary, 4)
	if err != nil || nonboundaryReport != baseline {
		t.Fatalf("nonboundary=%#v baseline=%#v error=%v", nonboundaryReport, baseline, err)
	}
	boundary := append([]byte(nil), mapping...)
	boundary[4]++
	boundaryReport, err := populateBleveReadMapping(t.Context(), boundary, 4)
	if err != nil || boundaryReport.pages != baseline.pages ||
		boundaryReport.evidence == baseline.evidence {
		t.Fatalf("boundary=%#v baseline=%#v error=%v", boundaryReport, baseline, err)
	}
}

func TestPopulateBleveReadMappingAcceptsEmptyMapping(t *testing.T) {
	report, err := populateBleveReadMapping(t.Context(), []byte{}, 4096)
	if err != nil || report != (bleveReadMappingPopulationReport{}) {
		t.Fatalf("report=%#v error=%v", report, err)
	}
}

func TestPopulateBleveReadMappingRefusesMissingInputs(t *testing.T) {
	if _, err := populateBleveReadMapping(t.Context(), nil, 4096); err == nil {
		t.Fatal("nil mapping accepted")
	}
	for _, pageSize := range []int{0, -1} {
		if _, err := populateBleveReadMapping(t.Context(), []byte{1}, pageSize); err == nil {
			t.Fatalf("page size %d accepted", pageSize)
		}
	}
}

func TestPopulateBleveReadMappingRefusesCancellationBeforeAndDuringPopulation(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := populateBleveReadMapping(
		canceled,
		[]byte{1},
		1,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("pre-canceled error=%v", err)
	}
	probe := &bleveReadMappingContextProbe{
		Context:                  t.Context(),
		errorsBeforeCancellation: 3,
	}
	report, err := populateBleveReadMapping(probe, []byte{1, 2, 3, 4}, 1)
	if !errors.Is(err, context.Canceled) || report.pages != 2 {
		t.Fatalf("report=%#v calls=%d error=%v", report, probe.calls, err)
	}
}

func TestBleveReadCacheActiveSegmentSelectsMappedPersistence(t *testing.T) {
	mapping := []byte{1, 2, 3}
	selected, persisted, err := bleveReadCacheActiveSegmentFrom(
		bleveReadCachePersistedSegmentProbe{
			path:    "/index/segment.zap",
			mapping: mapping,
		},
	)
	if err != nil || !persisted || selected.path != "/index/segment.zap" ||
		len(selected.mapping) != len(mapping) || &selected.mapping[0] != &mapping[0] {
		t.Fatalf("selected=%#v persisted=%v error=%v", selected, persisted, err)
	}
	empty, persisted, err := bleveReadCacheActiveSegmentFrom(
		bleveReadCachePersistedSegmentProbe{mapping: mapping},
	)
	if err != nil || persisted || empty.path != "" || empty.mapping != nil {
		t.Fatalf("empty=%#v persisted=%v error=%v", empty, persisted, err)
	}
	volatile, persisted, err := bleveReadCacheActiveSegmentFrom(struct{}{})
	if err != nil || persisted || volatile.path != "" || volatile.mapping != nil {
		t.Fatalf("volatile=%#v persisted=%v error=%v", volatile, persisted, err)
	}
}

func TestBleveReadCacheActiveSegmentsReturnPositionedFailure(t *testing.T) {
	_, err := bleveReadCacheActiveSegmentsFrom([]any{
		bleveReadCachePersistedSegmentProbe{
			path:    "/index/first.zap",
			mapping: []byte{1},
		},
		bleveReadCachePathOnlySegmentProbe{path: "/index/second.zap"},
	})
	if err == nil || !strings.Contains(err.Error(), "select active segment 1") {
		t.Fatalf("selection error=%v", err)
	}
}

func TestBleveReadCacheActiveSegmentRefusesUnavailableMapping(t *testing.T) {
	if _, persisted, err := bleveReadCacheActiveSegmentFrom(
		bleveReadCachePathOnlySegmentProbe{path: "/index/segment.zap"},
	); err == nil || persisted {
		t.Fatalf("path-only persisted=%v error=%v", persisted, err)
	}
	if _, persisted, err := bleveReadCacheActiveSegmentFrom(
		bleveReadCachePersistedSegmentProbe{path: "/index/segment.zap"},
	); err == nil || persisted {
		t.Fatalf("nil mapping persisted=%v error=%v", persisted, err)
	}
}

func TestBleveReadCacheLoadingRefusesFileMappingSizeMismatch(t *testing.T) {
	snapshot := &bleveReadCacheSnapshotProbe{paths: []string{"segment"}}
	loading := bleveReadCacheLoading{
		shards: []bleveReadCacheShard{&bleveReadCacheShardProbe{value: snapshot}},
		open: func(string) (bleveReadCacheFile, error) {
			return &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{
				bytes: 1,
				err:   io.EOF,
			}}}, nil
		},
	}
	if _, err := loading.run(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "file and mapping size differ") {
		t.Fatal("file mapping mismatch accepted")
	}
	if snapshot.closeCalls != 1 {
		t.Fatalf("snapshot closes=%d", snapshot.closeCalls)
	}
}
