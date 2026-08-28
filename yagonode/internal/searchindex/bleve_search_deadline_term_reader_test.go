package searchindex

import (
	"context"
	"errors"
	"testing"

	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveSearchDeadlineOptimizableTermReaderProbe struct {
	*bleveSearchDeadlineTermReaderProbe
	optimization      bleveindex.OptimizableContext
	err               error
	hook              func()
	kind              string
	received          bleveindex.OptimizableContext
	optimizationCalls int
}

func (probe *bleveSearchDeadlineOptimizableTermReaderProbe) Optimize(
	kind string,
	optimization bleveindex.OptimizableContext,
) (bleveindex.OptimizableContext, error) {
	probe.optimizationCalls++
	probe.kind = kind
	probe.received = optimization
	if probe.hook != nil {
		probe.hook()
	}

	return probe.optimization, probe.err
}

type bleveSearchDeadlineOptimizationProbe struct {
	optimized bleveindex.Optimized
	err       error
	hook      func()
	calls     int
}

func (probe *bleveSearchDeadlineOptimizationProbe) Finish() (
	bleveindex.Optimized,
	error,
) {
	probe.calls++
	if probe.hook != nil {
		probe.hook()
	}

	return probe.optimized, probe.err
}

func TestBleveSearchDeadlineTermReaderPreservesLiveOperations(t *testing.T) {
	nextDocument := &bleveindex.TermFieldDoc{Term: "next"}
	advanceDocument := &bleveindex.TermFieldDoc{Term: "advance"}
	probe := &bleveSearchDeadlineTermReaderProbe{
		nextDocument:    nextDocument,
		advanceDocument: advanceDocument,
		count:           17,
		size:            19,
	}
	reader := newBleveSearchDeadlineTermReader(t.Context(), probe)
	if _, ok := reader.(bleveindex.Optimizable); ok {
		t.Fatal("plain term reader gained optimization capability")
	}
	next, err := reader.Next(&bleveindex.TermFieldDoc{})
	if err != nil || next != nextDocument {
		t.Fatalf("next=%v error=%v", next, err)
	}
	identifier := bleveindex.NewIndexInternalID(nil, 23)
	advanced, err := reader.Advance(identifier, &bleveindex.TermFieldDoc{})
	if err != nil || advanced != advanceDocument || !probe.advanceID.Equals(identifier) {
		t.Fatalf("advance=%v id=%v error=%v", advanced, probe.advanceID, err)
	}
	count := reader.Count()
	size := reader.Size()
	if count != 17 || probe.countCalls != 1 || size != 19 {
		t.Fatalf("count/calls/size=%d/%d/%d", count, probe.countCalls, size)
	}
	if err := reader.Close(); err != nil || probe.closeCalls != 1 {
		t.Fatalf("close calls=%d error=%v", probe.closeCalls, err)
	}
}

func TestBleveSearchDeadlineTermReaderPreservesExhaustionAndFailures(t *testing.T) {
	nextErr := errors.New("next failed")
	advanceErr := errors.New("advance failed")
	closeErr := errors.New("close failed")
	probe := &bleveSearchDeadlineTermReaderProbe{
		nextErr:    nextErr,
		advanceErr: advanceErr,
		closeErr:   closeErr,
	}
	reader := newBleveSearchDeadlineTermReader(t.Context(), probe)
	if document, err := reader.Next(nil); document != nil || !errors.Is(err, nextErr) {
		t.Fatalf("next=%v error=%v", document, err)
	}
	if document, err := reader.Advance(nil, nil); document != nil || !errors.Is(err, advanceErr) {
		t.Fatalf("advance=%v error=%v", document, err)
	}
	probe.nextErr = nil
	probe.advanceErr = nil
	if document, err := reader.Next(nil); document != nil || err != nil {
		t.Fatalf("exhausted next=%v error=%v", document, err)
	}
	if document, err := reader.Advance(nil, nil); document != nil || err != nil {
		t.Fatalf("exhausted advance=%v error=%v", document, err)
	}
	if err := reader.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("close error=%v", err)
	}
}

func TestBleveSearchDeadlineTermReaderRefusesCanceledOperations(t *testing.T) {
	cause := errors.New("term read canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	probe := &bleveSearchDeadlineTermReaderProbe{count: 13}
	reader := newBleveSearchDeadlineTermReader(ctx, probe)
	if document, err := reader.Next(nil); document != nil || !errors.Is(err, cause) {
		t.Fatalf("next=%v error=%v", document, err)
	}
	if document, err := reader.Advance(nil, nil); document != nil || !errors.Is(err, cause) {
		t.Fatalf("advance=%v error=%v", document, err)
	}
	count := reader.Count()
	if count != 0 || probe.nextCalls != 0 || probe.advanceCalls != 0 || probe.countCalls != 0 {
		t.Fatalf(
			"count/calls=%d/%d/%d/%d",
			count,
			probe.nextCalls,
			probe.advanceCalls,
			probe.countCalls,
		)
	}
}

func TestBleveSearchDeadlineTermReaderStopsAfterActiveOperation(t *testing.T) {
	cause := errors.New("active read canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	probe := &bleveSearchDeadlineTermReaderProbe{
		nextDocument: &bleveindex.TermFieldDoc{Term: "discarded"},
		nextHook:     func() { cancel(cause) },
	}
	reader := newBleveSearchDeadlineTermReader(ctx, probe)
	if document, err := reader.Next(
		nil,
	); document != nil || !errors.Is(err, cause) ||
		probe.nextCalls != 1 {
		t.Fatalf("next=%v calls=%d error=%v", document, probe.nextCalls, err)
	}
	if document, err := reader.Next(
		nil,
	); document != nil || !errors.Is(err, cause) ||
		probe.nextCalls != 1 {
		t.Fatalf("second next=%v calls=%d error=%v", document, probe.nextCalls, err)
	}

	ctx, cancel = context.WithCancelCause(t.Context())
	probe = &bleveSearchDeadlineTermReaderProbe{
		advanceDocument: &bleveindex.TermFieldDoc{Term: "discarded"},
		advanceHook:     func() { cancel(cause) },
	}
	reader = newBleveSearchDeadlineTermReader(ctx, probe)
	if document, err := reader.Advance(nil, nil); document != nil ||
		!errors.Is(err, cause) || probe.advanceCalls != 1 {
		t.Fatalf("advance=%v calls=%d error=%v", document, probe.advanceCalls, err)
	}
	if document, err := reader.Advance(nil, nil); document != nil ||
		!errors.Is(err, cause) || probe.advanceCalls != 1 {
		t.Fatalf("second advance=%v calls=%d error=%v", document, probe.advanceCalls, err)
	}
}

func TestBleveSearchDeadlineTermReaderPreservesOptimization(t *testing.T) {
	innerOptimization := &bleveSearchDeadlineOptimizationProbe{}
	probe := &bleveSearchDeadlineOptimizableTermReaderProbe{
		bleveSearchDeadlineTermReaderProbe: &bleveSearchDeadlineTermReaderProbe{},
		optimization:                       innerOptimization,
	}
	reader := newBleveSearchDeadlineTermReader(t.Context(), probe)
	optimizable, ok := reader.(bleveindex.Optimizable)
	if !ok {
		t.Fatal("optimization capability missing")
	}
	prior := &bleveSearchDeadlineOptimizationProbe{}
	boundedPrior := bleveSearchDeadlineOptimizableContext{inner: prior, ctx: t.Context()}
	optimized, err := optimizable.Optimize("conjunction", boundedPrior)
	if err != nil {
		t.Fatal(err)
	}
	bounded, ok := optimized.(bleveSearchDeadlineOptimizableContext)
	if !ok || bounded.inner != innerOptimization || probe.received != prior ||
		probe.kind != "conjunction" || probe.optimizationCalls != 1 {
		t.Fatalf(
			"optimization=%T inner=%T received=%T kind=%s calls=%d",
			optimized,
			bounded.inner,
			probe.received,
			probe.kind,
			probe.optimizationCalls,
		)
	}
}

func TestBleveSearchDeadlineTermReaderPreservesOptimizationOutcomes(t *testing.T) {
	optimizationErr := errors.New("optimization failed")
	probe := &bleveSearchDeadlineOptimizableTermReaderProbe{
		bleveSearchDeadlineTermReaderProbe: &bleveSearchDeadlineTermReaderProbe{},
		err:                                optimizationErr,
	}
	optimizable := newBleveSearchDeadlineTermReader(t.Context(), probe).(bleveindex.Optimizable)
	if optimized, err := optimizable.Optimize("kind", nil); optimized != nil ||
		!errors.Is(err, optimizationErr) {
		t.Fatalf("failed optimization=%v error=%v", optimized, err)
	}
	probe.err = nil
	if optimized, err := optimizable.Optimize("kind", nil); optimized != nil || err != nil {
		t.Fatalf("empty optimization=%v error=%v", optimized, err)
	}
}

func TestBleveSearchDeadlineTermReaderRefusesCanceledOptimization(t *testing.T) {
	cause := errors.New("optimization canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	probe := &bleveSearchDeadlineOptimizableTermReaderProbe{
		bleveSearchDeadlineTermReaderProbe: &bleveSearchDeadlineTermReaderProbe{},
	}
	optimizable := newBleveSearchDeadlineTermReader(ctx, probe).(bleveindex.Optimizable)
	if optimized, err := optimizable.Optimize("kind", nil); optimized != nil ||
		!errors.Is(err, cause) || probe.optimizationCalls != 0 {
		t.Fatalf("optimization=%v calls=%d error=%v", optimized, probe.optimizationCalls, err)
	}

	ctx, cancel = context.WithCancelCause(t.Context())
	probe = &bleveSearchDeadlineOptimizableTermReaderProbe{
		bleveSearchDeadlineTermReaderProbe: &bleveSearchDeadlineTermReaderProbe{},
		optimization:                       &bleveSearchDeadlineOptimizationProbe{},
		hook:                               func() { cancel(cause) },
	}
	optimizable = newBleveSearchDeadlineTermReader(ctx, probe).(bleveindex.Optimizable)
	if optimized, err := optimizable.Optimize("kind", nil); optimized != nil ||
		!errors.Is(err, cause) || probe.optimizationCalls != 1 {
		t.Fatalf(
			"active optimization=%v calls=%d error=%v",
			optimized,
			probe.optimizationCalls,
			err,
		)
	}
}

func TestBleveSearchDeadlineOptimizationPreservesFinishedValues(t *testing.T) {
	value := struct{ name string }{name: "optimized"}
	probe := &bleveSearchDeadlineOptimizationProbe{optimized: value}
	optimization := bleveSearchDeadlineOptimizableContext{inner: probe, ctx: t.Context()}
	optimized, err := optimization.Finish()
	if err != nil || optimized != value || probe.calls != 1 {
		t.Fatalf("optimized=%v calls=%d error=%v", optimized, probe.calls, err)
	}

	termReader := &bleveSearchDeadlineTermReaderProbe{}
	probe = &bleveSearchDeadlineOptimizationProbe{optimized: termReader}
	optimization = bleveSearchDeadlineOptimizableContext{inner: probe, ctx: t.Context()}
	optimized, err = optimization.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := optimized.(bleveSearchDeadlineTermReader); !ok {
		t.Fatalf("optimized term reader=%T", optimized)
	}
}

func TestBleveSearchDeadlineOptimizationPreservesFinishFailure(t *testing.T) {
	finishErr := errors.New("finish failed")
	probe := &bleveSearchDeadlineOptimizationProbe{err: finishErr}
	optimization := bleveSearchDeadlineOptimizableContext{inner: probe, ctx: t.Context()}
	if optimized, err := optimization.Finish(); optimized != nil || !errors.Is(err, finishErr) {
		t.Fatalf("optimized=%v error=%v", optimized, err)
	}
}

func TestBleveSearchDeadlineOptimizationRefusesCancellationBeforeFinish(t *testing.T) {
	cause := errors.New("finish canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	probe := &bleveSearchDeadlineOptimizationProbe{}
	optimization := bleveSearchDeadlineOptimizableContext{inner: probe, ctx: ctx}
	if optimized, err := optimization.Finish(); optimized != nil ||
		!errors.Is(err, cause) || probe.calls != 0 {
		t.Fatalf("optimized=%v calls=%d error=%v", optimized, probe.calls, err)
	}
}

func TestBleveSearchDeadlineOptimizationClosesCanceledFinishedReader(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("optimized close failed")} {
		cause := errors.New("finish canceled after work")
		ctx, cancel := context.WithCancelCause(t.Context())
		termReader := &bleveSearchDeadlineTermReaderProbe{closeErr: closeErr}
		probe := &bleveSearchDeadlineOptimizationProbe{
			optimized: termReader,
			hook:      func() { cancel(cause) },
		}
		optimization := bleveSearchDeadlineOptimizableContext{inner: probe, ctx: ctx}
		optimized, err := optimization.Finish()
		if optimized != nil || !errors.Is(err, cause) || termReader.closeCalls != 1 ||
			(closeErr != nil && !errors.Is(err, closeErr)) {
			t.Fatalf(
				"close error=%v optimized=%v calls=%d error=%v",
				closeErr,
				optimized,
				termReader.closeCalls,
				err,
			)
		}
	}
}

func TestBleveSearchDeadlineOptimizationDiscardsCanceledFinishedValue(t *testing.T) {
	cause := errors.New("value finish canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	probe := &bleveSearchDeadlineOptimizationProbe{
		optimized: "value",
		hook:      func() { cancel(cause) },
	}
	optimization := bleveSearchDeadlineOptimizableContext{inner: probe, ctx: ctx}
	if optimized, err := optimization.Finish(); optimized != nil || !errors.Is(err, cause) {
		t.Fatalf("optimized=%v error=%v", optimized, err)
	}
}
