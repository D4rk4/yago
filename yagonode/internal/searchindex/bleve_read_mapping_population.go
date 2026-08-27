package searchindex

import (
	"context"
	"fmt"
	"math/bits"
)

type bleveReadCachePersistedPath interface {
	Path() string
}

type bleveReadCacheMappedData interface {
	Data() []byte
}

type bleveReadCacheActiveSegment struct {
	path    string
	mapping []byte
}

type bleveReadMappingPopulationReport struct {
	pages    uint64
	evidence uint64
}

func (s scorchBleveReadCacheSnapshot) activeSegments() (
	[]bleveReadCacheActiveSegment,
	error,
) {
	segments := s.snapshot.Segments()
	candidates := make([]any, len(segments))
	for position, segment := range segments {
		candidates[position] = segment.Segment()
	}

	return bleveReadCacheActiveSegmentsFrom(candidates)
}

func bleveReadCacheActiveSegmentsFrom(segments []any) (
	[]bleveReadCacheActiveSegment,
	error,
) {
	active := make([]bleveReadCacheActiveSegment, 0, len(segments))
	for position, segment := range segments {
		selected, persisted, err := bleveReadCacheActiveSegmentFrom(segment)
		if err != nil {
			return nil, fmt.Errorf("select active segment %d: %w", position, err)
		}
		if persisted {
			active = append(active, selected)
		}
	}

	return active, nil
}

func bleveReadCacheActiveSegmentFrom(segment any) (
	bleveReadCacheActiveSegment,
	bool,
	error,
) {
	persisted, ok := segment.(bleveReadCachePersistedPath)
	if !ok {
		return bleveReadCacheActiveSegment{}, false, nil
	}
	path := persisted.Path()
	if path == "" {
		return bleveReadCacheActiveSegment{}, false, nil
	}
	mapped, ok := segment.(bleveReadCacheMappedData)
	if !ok {
		return bleveReadCacheActiveSegment{}, false, fmt.Errorf(
			"persisted segment mapping unavailable",
		)
	}
	mapping := mapped.Data()
	if mapping == nil {
		return bleveReadCacheActiveSegment{}, false, fmt.Errorf("persisted segment mapping missing")
	}

	return bleveReadCacheActiveSegment{path: path, mapping: mapping}, true, nil
}

func populateBleveReadMapping(
	ctx context.Context,
	mapping []byte,
	pageSize int,
) (bleveReadMappingPopulationReport, error) {
	if err := ctx.Err(); err != nil {
		return bleveReadMappingPopulationReport{}, fmt.Errorf("populate mapping context: %w", err)
	}
	if mapping == nil {
		return bleveReadMappingPopulationReport{}, fmt.Errorf("populate mapping bytes required")
	}
	if pageSize <= 0 {
		return bleveReadMappingPopulationReport{}, fmt.Errorf("populate mapping page size required")
	}
	report := bleveReadMappingPopulationReport{}
	for offset := 0; offset < len(mapping); offset += pageSize {
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("populate mapping context: %w", err)
		}
		report.pages++
		report.evidence = bits.RotateLeft64(report.evidence, 7) ^
			uint64(mapping[offset]) ^ uint64(offset)
	}

	return report, nil
}
