package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/index/scorch/mergeplan"
	bleveindex "github.com/blevesearch/bleve_index_api"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type bleveReadSegmentShardProbe struct {
	states             []bleveReadSegmentState
	stateCalls         int
	consolidationCalls int
	stateError         error
	stateErrorAt       int
	consolidationError error
	cancel             context.CancelFunc
	order              *[]string
	identity           string
}

func (p *bleveReadSegmentShardProbe) state() (bleveReadSegmentState, error) {
	p.stateCalls++
	if p.order != nil {
		*p.order = append(*p.order, "state-"+p.identity)
	}
	if p.stateError != nil && p.stateCalls == p.stateErrorAt {
		return bleveReadSegmentState{}, p.stateError
	}
	position := min(p.stateCalls-1, len(p.states)-1)

	return p.states[position], nil
}

func (p *bleveReadSegmentShardProbe) consolidate(context.Context) error {
	p.consolidationCalls++
	if p.order != nil {
		*p.order = append(*p.order, "merge-"+p.identity)
	}
	if p.cancel != nil {
		p.cancel()
	}

	return p.consolidationError
}

type bleveReadSegmentAdmissionProbe struct {
	growthCalls   int
	headroomCalls int
	requiredBytes uint64
	growthError   error
	headroomError error
}

func (p *bleveReadSegmentAdmissionProbe) CheckGrowth() error {
	p.growthCalls++

	return p.growthError
}

func (p *bleveReadSegmentAdmissionProbe) CheckGrowthWithHeadroom(required uint64) error {
	p.headroomCalls++
	p.requiredBytes = required

	return p.headroomError
}

func TestBleveReadSegmentConsolidationLeavesBoundedShardsUnchanged(t *testing.T) {
	shard := &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{{
		documents: 50_000,
		segments:  1,
	}}}
	admission := &bleveReadSegmentAdmissionProbe{}
	measureCalls := 0
	consolidation := bleveReadSegmentConsolidation{
		root:      "unused",
		shards:    []bleveReadSegmentShard{shard},
		admission: admission,
		measure: func(string) (uint64, bool, error) {
			measureCalls++

			return 0, false, nil
		},
	}

	if err := consolidation.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if shard.consolidationCalls != 0 || measureCalls != 0 ||
		admission.growthCalls != 0 || admission.headroomCalls != 0 {
		t.Fatalf(
			"merge=%d measure=%d growth=%d headroom=%d",
			shard.consolidationCalls,
			measureCalls,
			admission.growthCalls,
			admission.headroomCalls,
		)
	}
}

func TestBleveReadSegmentConsolidationChecksHeadroomAndRunsSequentially(t *testing.T) {
	order := []string{}
	first := &bleveReadSegmentShardProbe{
		states: []bleveReadSegmentState{
			{documents: 70_000, segments: 10},
			{documents: 70_000, segments: 1},
		},
		order: &order, identity: "first",
	}
	second := &bleveReadSegmentShardProbe{
		states: []bleveReadSegmentState{
			{documents: 60_000, segments: 8},
			{documents: 60_000, segments: 1},
		},
		order: &order, identity: "second",
	}
	admission := &bleveReadSegmentAdmissionProbe{}
	consolidation := bleveReadSegmentConsolidation{
		root:      "index",
		shards:    []bleveReadSegmentShard{first, second},
		admission: admission,
		measure: func(root string) (uint64, bool, error) {
			if root != "index" {
				t.Fatalf("measured root=%q", root)
			}

			return 123, true, nil
		},
	}

	if err := consolidation.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if admission.headroomCalls != 1 || admission.growthCalls != 0 ||
		admission.requiredBytes != 123 {
		t.Fatalf("admission=%#v", admission)
	}
	wantOrder := []string{
		"state-first", "state-second", "merge-first", "state-first",
		"merge-second", "state-second",
	}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("operation order=%v, want=%v", order, wantOrder)
	}
}

func TestBleveReadSegmentConsolidationUsesGrowthFallback(t *testing.T) {
	shard := consolidatingBleveReadSegmentShard()
	admission := &bleveReadSegmentAdmissionProbe{}
	consolidation := bleveReadSegmentConsolidation{
		root:      "index",
		shards:    []bleveReadSegmentShard{shard},
		admission: admission,
		measure: func(string) (uint64, bool, error) {
			return 0, false, nil
		},
	}

	if err := consolidation.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if admission.growthCalls != 1 || admission.headroomCalls != 0 {
		t.Fatalf("admission=%#v", admission)
	}
}

func TestBleveReadSegmentConsolidationRefusesHeadroomFailure(t *testing.T) {
	sentinel := errors.New("headroom refused")
	shard := consolidatingBleveReadSegmentShard()
	admission := &bleveReadSegmentAdmissionProbe{headroomError: sentinel}
	consolidation := bleveReadSegmentConsolidation{
		root:      "index",
		shards:    []bleveReadSegmentShard{shard},
		admission: admission,
		measure: func(string) (uint64, bool, error) {
			return 10, true, nil
		},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("headroom error=%v, want=%v", err, sentinel)
	}
	if shard.consolidationCalls != 0 || admission.headroomCalls != 1 {
		t.Fatalf("merge=%d admission=%#v", shard.consolidationCalls, admission)
	}
}

func TestBleveReadSegmentConsolidationRefusesGrowthFailure(t *testing.T) {
	sentinel := errors.New("growth refused")
	shard := consolidatingBleveReadSegmentShard()
	admission := &bleveReadSegmentAdmissionProbe{growthError: sentinel}
	consolidation := bleveReadSegmentConsolidation{
		root:      "index",
		shards:    []bleveReadSegmentShard{shard},
		admission: admission,
		measure: func(string) (uint64, bool, error) {
			return 0, false, nil
		},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("growth error=%v, want=%v", err, sentinel)
	}
	if shard.consolidationCalls != 0 || admission.growthCalls != 1 {
		t.Fatalf("merge=%d admission=%#v", shard.consolidationCalls, admission)
	}
}

func TestBleveReadSegmentConsolidationRefusesMeasurementFailure(t *testing.T) {
	sentinel := errors.New("measurement failed")
	shard := consolidatingBleveReadSegmentShard()
	consolidation := bleveReadSegmentConsolidation{
		root:      "index",
		shards:    []bleveReadSegmentShard{shard},
		admission: &bleveReadSegmentAdmissionProbe{},
		measure: func(string) (uint64, bool, error) {
			return 0, false, sentinel
		},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("measurement error=%v, want=%v", err, sentinel)
	}
	if shard.consolidationCalls != 0 {
		t.Fatalf("merge calls=%d", shard.consolidationCalls)
	}
}

func TestBleveReadSegmentConsolidationReturnsInspectionFailure(t *testing.T) {
	sentinel := errors.New("inspect failed")
	shard := &bleveReadSegmentShardProbe{
		states:       []bleveReadSegmentState{{}},
		stateError:   sentinel,
		stateErrorAt: 1,
	}
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("inspection error=%v, want=%v", err, sentinel)
	}
}

func TestBleveReadSegmentConsolidationReturnsMergeFailure(t *testing.T) {
	sentinel := errors.New("merge failed")
	shard := consolidatingBleveReadSegmentShard()
	shard.consolidationError = sentinel
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("merge error=%v, want=%v", err, sentinel)
	}
}

func TestBleveReadSegmentConsolidationReturnsPostMergeInspectionFailure(t *testing.T) {
	sentinel := errors.New("post-merge inspect failed")
	shard := consolidatingBleveReadSegmentShard()
	shard.stateError = sentinel
	shard.stateErrorAt = 2
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("post-merge error=%v, want=%v", err, sentinel)
	}
}

func TestBleveReadSegmentConsolidationRefusesChangedDocuments(t *testing.T) {
	shard := &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{
		{documents: 70_000, segments: 10},
		{documents: 69_999, segments: 1},
	}}
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); err == nil {
		t.Fatal("changed document total accepted")
	}
}

func TestBleveReadSegmentConsolidationRefusesUnboundedStall(t *testing.T) {
	shard := &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{
		{documents: 100_000, segments: 5},
		{documents: 100_000, segments: 5},
	}}
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); err == nil {
		t.Fatal("unbounded stalled segments accepted")
	}
}

func TestBleveReadSegmentConsolidationAcceptsBoundedStall(t *testing.T) {
	shard := &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{
		{documents: 100_000, segments: 2},
		{documents: 100_000, segments: 2},
	}}
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if shard.consolidationCalls != 1 {
		t.Fatalf("merge calls=%d, want=1", shard.consolidationCalls)
	}
}

func TestBleveReadSegmentConsolidationRepeatsProgress(t *testing.T) {
	shard := &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{
		{documents: 70_000, segments: 10},
		{documents: 70_000, segments: 3},
		{documents: 70_000, segments: 1},
	}}
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if shard.consolidationCalls != 2 {
		t.Fatalf("merge calls=%d, want=2", shard.consolidationCalls)
	}
}

func TestBleveReadSegmentConsolidationObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	shard := consolidatingBleveReadSegmentShard()
	shard.cancel = cancel
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{shard},
	}

	if err := consolidation.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v, want=%v", err, context.Canceled)
	}
}

func TestBleveReadSegmentInspectionObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	consolidation := bleveReadSegmentConsolidation{
		shards: []bleveReadSegmentShard{consolidatingBleveReadSegmentShard()},
	}

	if err := consolidation.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v, want=%v", err, context.Canceled)
	}
}

func TestBleveReadSegmentShardObservesCancellationBeforeMerge(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	shard := consolidatingBleveReadSegmentShard()

	_, err := consolidateBleveReadSegmentShard(ctx, shard, shard.states[0])
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v, want=%v", err, context.Canceled)
	}
	if shard.consolidationCalls != 0 {
		t.Fatalf("merge calls=%d", shard.consolidationCalls)
	}
}

func TestBleveReadSegmentSpanBounds(t *testing.T) {
	for name, test := range map[string]struct {
		documents uint64
		desired   uint64
		maximum   uint64
	}{
		"empty":           {0, 0, 0},
		"one":             {1, 1, 1},
		"half-boundary":   {50_000, 1, 1},
		"half-over":       {50_001, 1, 2},
		"full-boundary":   {100_000, 1, 2},
		"full-over":       {100_001, 2, 3},
		"double-boundary": {200_000, 2, 4},
	} {
		t.Run(name, func(t *testing.T) {
			if got := desiredBleveReadSegments(test.documents); got != test.desired {
				t.Fatalf("desired segments=%d, want=%d", got, test.desired)
			}
			if got := maximumBleveReadSegments(test.documents); got != test.maximum {
				t.Fatalf("maximum segments=%d, want=%d", got, test.maximum)
			}
		})
	}
}

type advancedBleveIndexProbe struct {
	bleveIndexContract
	documents      uint64
	documentError  error
	implementation bleveindex.Index
	advancedError  error
}

func (p advancedBleveIndexProbe) DocCount() (uint64, error) {
	return p.documents, p.documentError
}

func (p advancedBleveIndexProbe) Advanced() (bleveindex.Index, error) {
	return p.implementation, p.advancedError
}

func TestScorchBleveReadSegmentShardReturnsDocumentCountFailure(t *testing.T) {
	sentinel := errors.New("count failed")
	shard := scorchBleveReadSegmentShard{index: advancedBleveIndexProbe{
		documentError: sentinel,
	}}

	if _, err := shard.state(); !errors.Is(err, sentinel) {
		t.Fatalf("state error=%v, want=%v", err, sentinel)
	}
}

func TestScorchBleveReadSegmentShardReturnsAdvancedFailure(t *testing.T) {
	sentinel := errors.New("advanced failed")
	shard := scorchBleveReadSegmentShard{index: advancedBleveIndexProbe{
		advancedError: sentinel,
	}}

	if _, err := shard.state(); !errors.Is(err, sentinel) {
		t.Fatalf("state error=%v, want=%v", err, sentinel)
	}
	if err := shard.consolidate(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("merge error=%v, want=%v", err, sentinel)
	}
}

func TestScorchBleveReadSegmentShardRefusesDifferentImplementation(t *testing.T) {
	shard := scorchBleveReadSegmentShard{index: advancedBleveIndexProbe{}}

	if _, err := shard.state(); err == nil {
		t.Fatal("non-scorch state accepted")
	}
	if err := shard.consolidate(t.Context()); err == nil {
		t.Fatal("non-scorch merge accepted")
	}
}

func TestScorchBleveReadSegmentShardRefusesMissingSegmentStatistic(t *testing.T) {
	shard := scorchBleveReadSegmentShard{index: advancedBleveIndexProbe{
		implementation: &scorch.Scorch{},
	}}

	if _, err := shard.state(); err == nil {
		t.Fatal("missing segment statistic accepted")
	}
}

func TestScorchBleveReadSegmentShardReturnsForceMergeFailure(t *testing.T) {
	original := forceBleveReadSegmentMerge
	t.Cleanup(func() { forceBleveReadSegmentMerge = original })
	sentinel := errors.New("force merge failed")
	forceBleveReadSegmentMerge = func(
		context.Context,
		*scorch.Scorch,
		*mergeplan.MergePlanOptions,
	) error {
		return sentinel
	}
	shard := scorchBleveReadSegmentShard{index: advancedBleveIndexProbe{
		implementation: &scorch.Scorch{},
	}}

	if err := shard.consolidate(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("force merge error=%v, want=%v", err, sentinel)
	}
}

func TestBleveDiskReopenConsolidatesReadSegments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	documents := sameBleveReadShardDocuments(6)
	directory := newFakeDocumentDirectory(documents...)
	writePersistedBleveReadSegments(t, root, directory, documents)

	reopened, err := NewBleveDiskIndex(t.Context(), root, directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := (scorchBleveReadSegmentShard{index: reopened.shards[0]}).state()
	if err != nil {
		t.Fatal(err)
	}
	if after.documents != uint64(len(documents)) || after.segments != 1 {
		t.Fatalf("consolidated state=%#v", after)
	}
	result, err := reopened.Search(t.Context(), SearchRequest{
		Query: "segmentword", MaxResults: len(documents), CandidateOnly: true,
	})
	if err != nil || result.Total != len(documents) || len(result.Results) != len(documents) {
		t.Fatalf("search result=%#v error=%v", result, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	unchanged, err := NewBleveDiskIndex(t.Context(), root, directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unchanged.Close() })
	state, err := (scorchBleveReadSegmentShard{index: unchanged.shards[0]}).state()
	if err != nil || state != after {
		t.Fatalf("unchanged state=%#v error=%v, want=%#v", state, err, after)
	}
}

func TestBleveDiskReopenRefusesConsolidationWithoutHeadroom(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	documents := sameBleveReadShardDocuments(6)
	directory := newFakeDocumentDirectory(documents...)
	writePersistedBleveReadSegments(t, root, directory, documents)
	sentinel := errors.New("insufficient headroom")
	admission := &bleveReadSegmentAdmissionProbe{headroomError: sentinel}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		root,
		directory,
		nil,
		admission,
	); !errors.Is(err, sentinel) {
		t.Fatalf("open error=%v, want=%v", err, sentinel)
	}
	if admission.headroomCalls != 1 || admission.growthCalls != 0 {
		t.Fatalf("admission=%#v", admission)
	}

	reopened, err := NewBleveDiskIndex(t.Context(), root, directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func consolidatingBleveReadSegmentShard() *bleveReadSegmentShardProbe {
	return &bleveReadSegmentShardProbe{states: []bleveReadSegmentState{
		{documents: 70_000, segments: 10},
		{documents: 70_000, segments: 1},
	}}
}

func sameBleveReadShardDocuments(total int) []documentstore.Document {
	documents := make([]documentstore.Document, 0, total)
	shards := make([]bleve.Index, diskShardCount)
	for candidate := 0; len(documents) < total; candidate++ {
		normalizedURL := fmt.Sprintf("https://example.org/segment/%d", candidate)
		if diskShardNumber(shards, normalizedURL) != 0 {
			continue
		}
		documents = append(documents, documentstore.Document{
			NormalizedURL: normalizedURL,
			Title:         "Segment document",
			ExtractedText: "segmentword",
		})
	}

	return documents
}

func writePersistedBleveReadSegments(
	t *testing.T,
	root string,
	directory *fakeDocumentDirectory,
	documents []documentstore.Document,
) {
	t.Helper()
	empty, err := NewBleveDiskIndex(t.Context(), root, directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	shardPath := diskShardPath(root, 0)
	if err := os.RemoveAll(shardPath); err != nil {
		t.Fatal(err)
	}
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatal(err)
	}
	configuration := bleveDiskScorchConfiguration()
	configuration["scorchMergePlanOptions"] = map[string]interface{}{
		"MaxSegmentsPerTier":   1000,
		"MaxSegmentSize":       diskMaxSegmentDocs,
		"SegmentsPerMergeTask": 10,
		"FloorSegmentSize":     1,
		"FloorSegmentFileSize": 1,
	}
	shard, err := bleve.NewUsing(
		shardPath,
		indexMapping,
		scorch.Name,
		scorch.Name,
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range documents {
		indexed, err := bleveDocumentFromStore(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := shard.Index(documentID(document), indexed); err != nil {
			t.Fatal(err)
		}
	}
	if err := shard.Close(); err != nil {
		t.Fatal(err)
	}
	persisted, err := openBleveDisk(shardPath)
	if err != nil {
		t.Fatal(err)
	}
	persistedState, err := (scorchBleveReadSegmentShard{index: persisted}).state()
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.Close(); err != nil {
		t.Fatal(err)
	}
	if persistedState.segments != uint64(len(documents)) {
		t.Fatalf(
			"persisted fixture root segments=%d, want=%d",
			persistedState.segments,
			len(documents),
		)
	}
}
