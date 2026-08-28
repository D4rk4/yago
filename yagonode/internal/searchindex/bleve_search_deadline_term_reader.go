package searchindex

import (
	"context"
	"fmt"

	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveSearchDeadlineTermReader struct {
	inner bleveindex.TermFieldReader
	ctx   context.Context
}

func newBleveSearchDeadlineTermReader(
	ctx context.Context,
	reader bleveindex.TermFieldReader,
) bleveindex.TermFieldReader {
	bounded := bleveSearchDeadlineTermReader{inner: reader, ctx: ctx}
	optimizable, available := reader.(bleveindex.Optimizable)
	if !available {
		return bounded
	}

	return bleveSearchDeadlineOptimizableTermReader{
		bleveSearchDeadlineTermReader: bounded,
		optimizable:                   optimizable,
	}
}

func (reader bleveSearchDeadlineTermReader) Next(
	preallocated *bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term reader context: %w", cause)
	}
	document, err := reader.inner.Next(preallocated)
	if err != nil {
		return nil, fmt.Errorf("read next term document: %w", err)
	}
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term reader context: %w", cause)
	}

	return document, nil
}

func (reader bleveSearchDeadlineTermReader) Advance(
	identifier bleveindex.IndexInternalID,
	preallocated *bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term reader context: %w", cause)
	}
	document, err := reader.inner.Advance(identifier, preallocated)
	if err != nil {
		return nil, fmt.Errorf("advance term reader: %w", err)
	}
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term reader context: %w", cause)
	}

	return document, nil
}

func (reader bleveSearchDeadlineTermReader) Count() uint64 {
	if context.Cause(reader.ctx) != nil {
		return 0
	}

	return reader.inner.Count()
}

func (reader bleveSearchDeadlineTermReader) Close() error {
	if err := reader.inner.Close(); err != nil {
		return fmt.Errorf("close term reader: %w", err)
	}

	return nil
}

func (reader bleveSearchDeadlineTermReader) Size() int {
	return reader.inner.Size()
}

type bleveSearchDeadlineOptimizableTermReader struct {
	bleveSearchDeadlineTermReader
	optimizable bleveindex.Optimizable
}

func (reader bleveSearchDeadlineOptimizableTermReader) Optimize(
	kind string,
	optimization bleveindex.OptimizableContext,
) (bleveindex.OptimizableContext, error) {
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term optimization context: %w", cause)
	}
	if bounded, ok := optimization.(bleveSearchDeadlineOptimizableContext); ok {
		optimization = bounded.inner
	}
	optimized, err := reader.optimizable.Optimize(kind, optimization)
	if err != nil {
		return nil, fmt.Errorf("optimize term reader: %w", err)
	}
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term optimization context: %w", cause)
	}
	if optimized == nil {
		return nil, nil
	}

	return bleveSearchDeadlineOptimizableContext{
		inner: optimized,
		ctx:   reader.ctx,
	}, nil
}

type bleveSearchDeadlineOptimizableContext struct {
	inner bleveindex.OptimizableContext
	ctx   context.Context
}

func (optimization bleveSearchDeadlineOptimizableContext) Finish() (
	bleveindex.Optimized,
	error,
) {
	if cause := context.Cause(optimization.ctx); cause != nil {
		return nil, fmt.Errorf("term optimization context: %w", cause)
	}
	optimized, err := optimization.inner.Finish()
	if err != nil {
		return nil, fmt.Errorf("finish term optimization: %w", err)
	}
	if cause := context.Cause(optimization.ctx); cause != nil {
		if reader, ok := optimized.(bleveindex.TermFieldReader); ok {
			closeErr := reader.Close()
			return nil, bleveSearchDeadlineClosureError(
				"term optimization context",
				cause,
				"close canceled optimized term reader",
				closeErr,
			)
		}

		return nil, fmt.Errorf("term optimization context: %w", cause)
	}
	reader, ok := optimized.(bleveindex.TermFieldReader)
	if !ok {
		return optimized, nil
	}

	return newBleveSearchDeadlineTermReader(optimization.ctx, reader), nil
}

var (
	_ bleveindex.TermFieldReader    = bleveSearchDeadlineTermReader{}
	_ bleveindex.TermFieldReader    = bleveSearchDeadlineOptimizableTermReader{}
	_ bleveindex.Optimizable        = bleveSearchDeadlineOptimizableTermReader{}
	_ bleveindex.OptimizableContext = bleveSearchDeadlineOptimizableContext{}
)
