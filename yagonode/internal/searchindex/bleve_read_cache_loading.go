package searchindex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

const (
	bleveReadCacheWindowBytes             = 4 * 1024 * 1024
	bleveReadCacheLoadingStartedMessage   = "bleve read cache loading started"
	bleveReadCacheLoadingCompletedMessage = "bleve read cache loading completed"
)

type bleveReadCacheFile interface {
	Read([]byte) (int, error)
	Close() error
}

type bleveReadCacheSnapshot interface {
	activeSegments() ([]bleveReadCacheActiveSegment, error)
	Close() error
}

type bleveReadCacheShard interface {
	snapshot() (bleveReadCacheSnapshot, error)
}

type bleveReadCacheSnapshotSource interface {
	Reader() (bleveindex.IndexReader, error)
}

type scorchBleveReadCacheShard struct {
	index bleve.Index
}

type scorchBleveReadCacheSnapshot struct {
	snapshot *scorch.IndexSnapshot
}

type bleveReadCacheLoading struct {
	shards []bleveReadCacheShard
	open   func(string) (bleveReadCacheFile, error)
}

type bleveReadCacheLoadingReport struct {
	segments        int
	bytes           uint64
	mappingPages    uint64
	mappingEvidence uint64
}

func loadBleveReadCache(ctx context.Context, indexes []bleve.Index) error {
	shards := make([]bleveReadCacheShard, len(indexes))
	for position, index := range indexes {
		shards[position] = scorchBleveReadCacheShard{index: index}
	}
	loading := bleveReadCacheLoading{shards: shards, open: openBleveReadCacheSegment}
	slog.DebugContext(ctx, bleveReadCacheLoadingStartedMessage, slog.Int("shards", len(shards)))
	report, err := loading.run(ctx)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, bleveReadCacheLoadingCompletedMessage,
		slog.Int("shards", len(shards)),
		slog.Int("segments", report.segments),
		slog.Uint64("bytes", report.bytes),
		slog.Uint64("mappingPages", report.mappingPages),
		slog.Uint64("mappingEvidence", report.mappingEvidence),
	)

	return nil
}

func (l bleveReadCacheLoading) run(ctx context.Context) (bleveReadCacheLoadingReport, error) {
	if err := ctx.Err(); err != nil {
		return bleveReadCacheLoadingReport{}, fmt.Errorf("load bleve read cache: %w", err)
	}
	window := make([]byte, bleveReadCacheWindowBytes)
	report := bleveReadCacheLoadingReport{}
	for position, shard := range l.shards {
		if err := ctx.Err(); err != nil {
			return bleveReadCacheLoadingReport{}, fmt.Errorf("load bleve read cache: %w", err)
		}
		snapshot, err := shard.snapshot()
		if err != nil {
			return bleveReadCacheLoadingReport{}, fmt.Errorf(
				"open bleve read cache shard %d: %w",
				position,
				err,
			)
		}
		loaded, err := l.loadSnapshot(ctx, snapshot, window)
		if err != nil {
			return bleveReadCacheLoadingReport{}, fmt.Errorf(
				"load bleve read cache shard %d: %w",
				position,
				err,
			)
		}
		report.segments += loaded.segments
		report.bytes += loaded.bytes
		report.mappingPages += loaded.mappingPages
		report.mappingEvidence ^= loaded.mappingEvidence
	}

	return report, nil
}

func (l bleveReadCacheLoading) loadSnapshot(
	ctx context.Context,
	snapshot bleveReadCacheSnapshot,
	window []byte,
) (report bleveReadCacheLoadingReport, err error) {
	defer func() {
		err = errors.Join(err, snapshot.Close())
	}()
	segments, err := snapshot.activeSegments()
	if err != nil {
		return bleveReadCacheLoadingReport{}, err
	}
	for position, segment := range segments {
		loaded, loadErr := loadBleveReadCacheSegment(ctx, segment.path, window, l.open)
		if loadErr != nil {
			return bleveReadCacheLoadingReport{}, fmt.Errorf(
				"load active segment %d: %w",
				position,
				loadErr,
			)
		}
		if loaded != uint64(len(segment.mapping)) {
			return bleveReadCacheLoadingReport{}, fmt.Errorf(
				"load active segment %d: file and mapping size differ",
				position,
			)
		}
		mapping, mappingErr := populateBleveReadMapping(
			ctx,
			segment.mapping,
			os.Getpagesize(),
		)
		if mappingErr != nil {
			return bleveReadCacheLoadingReport{}, fmt.Errorf(
				"populate active segment %d mapping: %w",
				position,
				mappingErr,
			)
		}
		report.segments++
		report.bytes += loaded
		report.mappingPages += mapping.pages
		report.mappingEvidence ^= mapping.evidence
	}

	return report, nil
}

func loadBleveReadCacheSegment(
	ctx context.Context,
	path string,
	window []byte,
	open func(string) (bleveReadCacheFile, error),
) (loaded uint64, err error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("load active segment context: %w", err)
	}
	file, err := open(path)
	if err != nil {
		return 0, fmt.Errorf("open active segment: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	for {
		if err := ctx.Err(); err != nil {
			return loaded, fmt.Errorf("load active segment context: %w", err)
		}
		read, readErr := file.Read(window)
		readBytes, conversionErr := bleveReadCacheBytes(read, len(window))
		if conversionErr != nil {
			return loaded, conversionErr
		}
		loaded += readBytes
		if errors.Is(readErr, io.EOF) {
			return loaded, nil
		}
		if readErr != nil {
			return loaded, fmt.Errorf("read active segment: %w", readErr)
		}
		if read == 0 {
			return loaded, io.ErrNoProgress
		}
	}
}

func openBleveReadCacheSegment(path string) (bleveReadCacheFile, error) {
	file, err := os.OpenInRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("open segment within index root: %w", err)
	}

	return file, nil
}

func (s scorchBleveReadCacheShard) snapshot() (bleveReadCacheSnapshot, error) {
	scorchIndex, err := bleveScorchImplementation(s.index)
	if err != nil {
		return nil, err
	}

	return openScorchBleveReadCacheSnapshot(scorchIndex)
}

func openScorchBleveReadCacheSnapshot(
	source bleveReadCacheSnapshotSource,
) (bleveReadCacheSnapshot, error) {
	reader, err := source.Reader()
	if err != nil {
		return nil, fmt.Errorf("open scorch snapshot: %w", err)
	}
	snapshot, ok := reader.(*scorch.IndexSnapshot)
	if ok && snapshot != nil {
		return scorchBleveReadCacheSnapshot{snapshot: snapshot}, nil
	}
	if reader == nil || ok {
		return nil, fmt.Errorf("scorch snapshot unavailable")
	}

	return nil, errors.Join(
		fmt.Errorf("scorch snapshot unavailable"),
		reader.Close(),
	)
}

func (s scorchBleveReadCacheSnapshot) Close() error {
	return wrapBleveReadCacheSnapshotClose(s.snapshot.Close())
}

func bleveReadCacheBytes(read, window int) (uint64, error) {
	if read < 0 {
		return 0, fmt.Errorf("active segment returned negative read size")
	}
	if read > window {
		return 0, fmt.Errorf("active segment returned read size beyond window")
	}

	return uint64(read), nil
}

func wrapBleveReadCacheSnapshotClose(err error) error {
	if err != nil {
		return fmt.Errorf("close scorch snapshot: %w", err)
	}

	return nil
}
