package searchindex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveReadCacheShardProbe struct {
	value bleveReadCacheSnapshot
	err   error
	calls int
}

func (p *bleveReadCacheShardProbe) snapshot() (bleveReadCacheSnapshot, error) {
	p.calls++

	return p.value, p.err
}

type bleveReadCacheSnapshotProbe struct {
	paths      []string
	closeError error
	closeCalls int
	onClose    func()
}

func (p *bleveReadCacheSnapshotProbe) activeSegmentPaths() []string {
	return p.paths
}

func (p *bleveReadCacheSnapshotProbe) Close() error {
	p.closeCalls++
	if p.onClose != nil {
		p.onClose()
	}

	return p.closeError
}

type bleveReadCacheFileRead struct {
	bytes int
	err   error
}

type bleveReadCacheFileProbe struct {
	reads      []bleveReadCacheFileRead
	readCalls  int
	closeError error
	closeCalls int
	onRead     func(int)
	onClose    func()
}

func (p *bleveReadCacheFileProbe) Read(destination []byte) (int, error) {
	position := min(p.readCalls, len(p.reads)-1)
	read := p.reads[position]
	p.readCalls++
	for index := 0; index < min(read.bytes, len(destination)); index++ {
		destination[index] = 'x'
	}
	if p.onRead != nil {
		p.onRead(p.readCalls)
	}

	return read.bytes, read.err
}

func (p *bleveReadCacheFileProbe) Close() error {
	p.closeCalls++
	if p.onClose != nil {
		p.onClose()
	}

	return p.closeError
}

type bleveReadCacheSnapshotSourceProbe struct {
	reader bleveindex.IndexReader
	err    error
}

func (p bleveReadCacheSnapshotSourceProbe) Reader() (bleveindex.IndexReader, error) {
	return p.reader, p.err
}

type differentBleveReadCacheReaderProbe struct {
	bleveindex.IndexReader
	closeError error
	closeCalls int
}

func (p *differentBleveReadCacheReaderProbe) Close() error {
	p.closeCalls++

	return p.closeError
}

type bleveReadCachePersistedSegmentProbe struct {
	path string
}

func (p bleveReadCachePersistedSegmentProbe) Path() string {
	return p.path
}

func TestBleveReadCacheLoadingLoadsActiveSegmentsSequentially(t *testing.T) {
	first := &bleveReadCacheSnapshotProbe{paths: []string{"alpha", "bravo"}}
	second := &bleveReadCacheSnapshotProbe{paths: []string{"charlie"}}
	opened := []string{}
	files := []*bleveReadCacheFileProbe{}
	loading := bleveReadCacheLoading{
		shards: []bleveReadCacheShard{
			&bleveReadCacheShardProbe{value: first},
			&bleveReadCacheShardProbe{value: second},
		},
		open: func(path string) (bleveReadCacheFile, error) {
			opened = append(opened, path)
			file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{
				bytes: len(path),
				err:   io.EOF,
			}}}
			files = append(files, file)

			return file, nil
		},
	}

	report, err := loading.run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opened, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("open order=%v", opened)
	}
	if report.segments != 3 || report.bytes != uint64(len("alphabravocharlie")) {
		t.Fatalf("report=%#v", report)
	}
	if first.closeCalls != 1 || second.closeCalls != 1 {
		t.Fatalf("snapshot closes=%d,%d", first.closeCalls, second.closeCalls)
	}
	for position, file := range files {
		if file.closeCalls != 1 {
			t.Fatalf("file %d closes=%d", position, file.closeCalls)
		}
	}
}

func TestBleveReadCacheLoadingRefusesCanceledContextBeforeSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	shard := &bleveReadCacheShardProbe{}
	loading := bleveReadCacheLoading{shards: []bleveReadCacheShard{shard}}

	if _, err := loading.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("loading error=%v", err)
	}
	if shard.calls != 0 {
		t.Fatalf("snapshot calls=%d", shard.calls)
	}
}

func TestBleveReadCacheLoadingStopsWhenPriorSnapshotCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := &bleveReadCacheSnapshotProbe{onClose: cancel}
	second := &bleveReadCacheShardProbe{}
	loading := bleveReadCacheLoading{
		shards: []bleveReadCacheShard{
			&bleveReadCacheShardProbe{value: first},
			second,
		},
	}

	if _, err := loading.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("loading error=%v", err)
	}
	if first.closeCalls != 1 || second.calls != 0 {
		t.Fatalf("first closes=%d second calls=%d", first.closeCalls, second.calls)
	}
}

func TestBleveReadCacheLoadingStopsAtSnapshotFailure(t *testing.T) {
	sentinel := errors.New("snapshot failed")
	first := &bleveReadCacheShardProbe{err: sentinel}
	second := &bleveReadCacheShardProbe{}
	loading := bleveReadCacheLoading{shards: []bleveReadCacheShard{first, second}}

	if _, err := loading.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("loading error=%v", err)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("snapshot calls=%d,%d", first.calls, second.calls)
	}
}

func TestBleveReadCacheLoadingReturnsReadFileAndSnapshotFailures(t *testing.T) {
	readFailure := errors.New("read failed")
	fileCloseFailure := errors.New("file close failed")
	snapshotCloseFailure := errors.New("snapshot close failed")
	file := &bleveReadCacheFileProbe{
		reads:      []bleveReadCacheFileRead{{bytes: 2, err: readFailure}},
		closeError: fileCloseFailure,
	}
	snapshot := &bleveReadCacheSnapshotProbe{
		paths:      []string{"segment"},
		closeError: snapshotCloseFailure,
	}
	loading := bleveReadCacheLoading{
		shards: []bleveReadCacheShard{&bleveReadCacheShardProbe{value: snapshot}},
		open: func(string) (bleveReadCacheFile, error) {
			return file, nil
		},
	}

	_, err := loading.run(t.Context())
	for _, expected := range []error{readFailure, fileCloseFailure, snapshotCloseFailure} {
		if !errors.Is(err, expected) {
			t.Fatalf("loading error=%v, want=%v", err, expected)
		}
	}
	if file.closeCalls != 1 || snapshot.closeCalls != 1 {
		t.Fatalf("closes=%d,%d", file.closeCalls, snapshot.closeCalls)
	}
}

func TestBleveReadCacheLoadingReturnsSnapshotCloseFailure(t *testing.T) {
	sentinel := errors.New("snapshot close failed")
	snapshot := &bleveReadCacheSnapshotProbe{closeError: sentinel}
	loading := bleveReadCacheLoading{
		shards: []bleveReadCacheShard{&bleveReadCacheShardProbe{value: snapshot}},
	}

	if _, err := loading.run(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("loading error=%v", err)
	}
}

func TestLoadBleveReadCacheSegmentReadsToEndAndCloses(t *testing.T) {
	file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{
		{bytes: 3},
		{bytes: 2, err: io.EOF},
	}}
	loaded, err := loadBleveReadCacheSegment(
		t.Context(),
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if err != nil || loaded != 5 || file.closeCalls != 1 {
		t.Fatalf("loaded=%d closes=%d error=%v", loaded, file.closeCalls, err)
	}
}

func TestLoadBleveReadCacheSegmentAcceptsEmptyFile(t *testing.T) {
	file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{err: io.EOF}}}
	loaded, err := loadBleveReadCacheSegment(
		t.Context(),
		"empty",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if err != nil || loaded != 0 || file.closeCalls != 1 {
		t.Fatalf("loaded=%d closes=%d error=%v", loaded, file.closeCalls, err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesOpenFailure(t *testing.T) {
	sentinel := errors.New("open failed")
	_, err := loadBleveReadCacheSegment(
		t.Context(),
		"missing",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return nil, sentinel },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("load error=%v", err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesNoProgress(t *testing.T) {
	file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{}}}
	_, err := loadBleveReadCacheSegment(
		t.Context(),
		"stalled",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if !errors.Is(err, io.ErrNoProgress) || file.closeCalls != 1 {
		t.Fatalf("closes=%d error=%v", file.closeCalls, err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesCancellationBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	openCalls := 0
	_, err := loadBleveReadCacheSegment(
		ctx,
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) {
			openCalls++

			return nil, nil
		},
	)
	if !errors.Is(err, context.Canceled) || openCalls != 0 {
		t.Fatalf("open calls=%d error=%v", openCalls, err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesCancellationBetweenReads(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	file := &bleveReadCacheFileProbe{
		reads: []bleveReadCacheFileRead{{bytes: 1}, {err: io.EOF}},
		onRead: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	loaded, err := loadBleveReadCacheSegment(
		ctx,
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if !errors.Is(err, context.Canceled) || loaded != 1 ||
		file.readCalls != 1 || file.closeCalls != 1 {
		t.Fatalf(
			"loaded=%d reads=%d closes=%d error=%v",
			loaded,
			file.readCalls,
			file.closeCalls,
			err,
		)
	}
}

func TestLoadBleveReadCacheSegmentReturnsCloseFailure(t *testing.T) {
	sentinel := errors.New("close failed")
	file := &bleveReadCacheFileProbe{
		reads:      []bleveReadCacheFileRead{{err: io.EOF}},
		closeError: sentinel,
	}
	_, err := loadBleveReadCacheSegment(
		t.Context(),
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("load error=%v", err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesNegativeReadSize(t *testing.T) {
	file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{bytes: -1}}}
	_, err := loadBleveReadCacheSegment(
		t.Context(),
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "negative read size") || file.closeCalls != 1 {
		t.Fatalf("closes=%d error=%v", file.closeCalls, err)
	}
}

func TestLoadBleveReadCacheSegmentRefusesReadSizeBeyondWindow(t *testing.T) {
	file := &bleveReadCacheFileProbe{reads: []bleveReadCacheFileRead{{bytes: 17}}}
	_, err := loadBleveReadCacheSegment(
		t.Context(),
		"segment",
		make([]byte, 16),
		func(string) (bleveReadCacheFile, error) { return file, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "read size beyond window") ||
		file.closeCalls != 1 {
		t.Fatalf("closes=%d error=%v", file.closeCalls, err)
	}
}

func TestOpenScorchBleveReadCacheSnapshotReturnsReaderFailure(t *testing.T) {
	sentinel := errors.New("reader failed")
	_, err := openScorchBleveReadCacheSnapshot(bleveReadCacheSnapshotSourceProbe{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("snapshot error=%v", err)
	}
}

func TestOpenScorchBleveReadCacheSnapshotRefusesMissingReader(t *testing.T) {
	if _, err := openScorchBleveReadCacheSnapshot(bleveReadCacheSnapshotSourceProbe{}); err == nil {
		t.Fatal("missing snapshot accepted")
	}
}

func TestOpenScorchBleveReadCacheSnapshotRefusesTypedNilSnapshot(t *testing.T) {
	var missing *scorch.IndexSnapshot
	if _, err := openScorchBleveReadCacheSnapshot(bleveReadCacheSnapshotSourceProbe{
		reader: missing,
	}); err == nil {
		t.Fatal("typed nil snapshot accepted")
	}
}

func TestOpenScorchBleveReadCacheSnapshotClosesDifferentReader(t *testing.T) {
	sentinel := errors.New("different reader close failed")
	reader := &differentBleveReadCacheReaderProbe{closeError: sentinel}
	_, err := openScorchBleveReadCacheSnapshot(bleveReadCacheSnapshotSourceProbe{
		reader: reader,
	})
	if err == nil || !errors.Is(err, sentinel) || reader.closeCalls != 1 {
		t.Fatalf("closes=%d error=%v", reader.closeCalls, err)
	}
}

func TestBleveReadCacheSegmentPathSelectsOnlyPersistedPath(t *testing.T) {
	path, persisted := bleveReadCacheSegmentPath(bleveReadCachePersistedSegmentProbe{
		path: "/index/segment.zap",
	})
	if !persisted || path != "/index/segment.zap" {
		t.Fatalf("path=%q persisted=%v", path, persisted)
	}
	path, persisted = bleveReadCacheSegmentPath(bleveReadCachePersistedSegmentProbe{})
	if persisted || path != "" {
		t.Fatalf("empty path=%q persisted=%v", path, persisted)
	}
	if path, persisted := bleveReadCacheSegmentPath(struct{}{}); persisted || path != "" {
		t.Fatalf("volatile path=%q persisted=%v", path, persisted)
	}
}

func TestBleveReadCacheBytesAcceptsSizeWithinWindow(t *testing.T) {
	converted, err := bleveReadCacheBytes(42, 42)
	if err != nil || converted != 42 {
		t.Fatalf("converted=%d error=%v", converted, err)
	}
	if _, err := bleveReadCacheBytes(-1, 42); err == nil {
		t.Fatal("negative read size accepted")
	}
	if _, err := bleveReadCacheBytes(43, 42); err == nil {
		t.Fatal("read size beyond window accepted")
	}
}

func TestWrapBleveReadCacheSnapshotClosePreservesOutcome(t *testing.T) {
	if err := wrapBleveReadCacheSnapshotClose(nil); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("close failed")
	if err := wrapBleveReadCacheSnapshotClose(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("close error=%v", err)
	}
}

func TestOpenBleveReadCacheSegmentLeavesFileUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "segment.zap")
	want := bytes.Repeat([]byte("persisted-segment"), 1024)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	wantTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBleveReadCacheSegment(
		t.Context(),
		path,
		make([]byte, 4096),
		openBleveReadCacheSegment,
	)
	if err != nil || loaded != uint64(len(want)) {
		t.Fatalf("loaded=%d error=%v", loaded, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	directoryRoot, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	content, err := directoryRoot.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := directoryRoot.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, want) || before.Size() != after.Size() ||
		before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("file changed before=%#v after=%#v", before, after)
	}
}

func TestOpenBleveReadCacheSegmentReturnsMissingPath(t *testing.T) {
	if _, err := openBleveReadCacheSegment(filepath.Join(t.TempDir(), "missing.zap")); err == nil {
		t.Fatal("missing path opened")
	}
}

func TestLoadBleveReadCacheUsesOnlyActiveScorchRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	documents := sameBleveReadShardDocuments(1)
	writePersistedBleveReadSegments(t, root, newFakeDocumentDirectory(documents...), documents)
	shard, err := openBleveDisk(diskShardPath(root, 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shard.Close() })
	scorchIndex, err := bleveScorchImplementation(shard)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := openScorchBleveReadCacheSnapshot(scorchIndex)
	if err != nil {
		t.Fatal(err)
	}
	paths := snapshot.activeSegmentPaths()
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Ext(paths[0]) != ".zap" {
		t.Fatalf("active paths=%v", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err := loadBleveReadCache(t.Context(), []bleve.Index{shard}); err != nil {
		t.Fatal(err)
	}
	if err := loadBleveReadCache(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestLoadBleveReadCacheReturnsScorchImplementationFailure(t *testing.T) {
	sentinel := errors.New("advanced failed")
	err := loadBleveReadCache(t.Context(), []bleve.Index{advancedBleveIndexProbe{
		advancedError: sentinel,
	}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("loading error=%v", err)
	}
}

func TestScorchBleveReadCacheShardRefusesUnavailableSnapshot(t *testing.T) {
	shard := scorchBleveReadCacheShard{index: advancedBleveIndexProbe{
		implementation: &scorch.Scorch{},
	}}
	if _, err := shard.snapshot(); err == nil {
		t.Fatal("unavailable snapshot accepted")
	}
}

func TestPrepareBleveReadsRefusesMissingActiveRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	documents := sameBleveReadShardDocuments(1)
	writePersistedBleveReadSegments(t, root, newFakeDocumentDirectory(documents...), documents)
	shard, err := openBleveDisk(diskShardPath(root, 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shard.Close() })
	scorchIndex, err := bleveScorchImplementation(shard)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := openScorchBleveReadCacheSnapshot(scorchIndex)
	if err != nil {
		t.Fatal(err)
	}
	paths := snapshot.activeSegmentPaths()
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("active paths=%v", paths)
	}
	missing := paths[0] + ".missing"
	if err := os.Rename(paths[0], missing); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(missing, paths[0]) })
	index := &BleveDiskIndex{shards: []bleve.Index{shard}, alias: bleve.NewIndexAlias(shard)}
	if err := index.prepareBleveReads(t.Context(), root, nil); err == nil ||
		!strings.Contains(err.Error(), "open active segment") {
		t.Fatalf("prepare error=%v", err)
	}
}
