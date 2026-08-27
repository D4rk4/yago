package searchindex

import (
	"context"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const bleveReadMappingBenchmarkDocuments = 100_000

func BenchmarkBleveReadMappingFirstQuery(b *testing.B) {
	root := b.TempDir()
	buildBleveReadMappingBenchmarkIndex(b, root)
	for _, preparation := range []struct {
		name string
		run  func(context.Context, []bleve.Index) error
	}{
		{name: "file-cache-only", run: prepareBleveReadMappingBenchmarkFiles},
		{name: "live-mapping", run: loadBleveReadCache},
	} {
		b.Run(preparation.name, func(b *testing.B) {
			b.ReportAllocs()
			b.StopTimer()
			var minorFaults int64
			for range b.N {
				shards := openBleveReadMappingBenchmarkShards(b, root)
				if err := preparation.run(b.Context(), shards); err != nil {
					closeBleveShards(shards)
					b.Fatal(err)
				}
				request := bleve.NewSearchRequest(bleveSearchQuery(
					SearchRequest{
						Query:      "economics",
						Terms:      []string{"economics"},
						MaxResults: 10,
					},
					true,
					true,
				))
				request.Size = 10
				alias := bleve.NewIndexAlias(shards...)
				before := bleveReadMappingBenchmarkUsage(b)
				b.StartTimer()
				result, err := alias.SearchInContext(b.Context(), request)
				b.StopTimer()
				after := bleveReadMappingBenchmarkUsage(b)
				minorFaults += after.Minflt - before.Minflt
				closeBleveShards(shards)
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Hits) != 10 {
					b.Fatalf("hits=%d", len(result.Hits))
				}
			}
			b.ReportMetric(float64(minorFaults)/float64(b.N), "minor-faults/op")
		})
	}
}

func bleveReadMappingBenchmarkUsage(b *testing.B) syscall.Rusage {
	b.Helper()
	usage := syscall.Rusage{}
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		b.Fatal(err)
	}

	return usage
}

func buildBleveReadMappingBenchmarkIndex(b *testing.B, root string) {
	b.Helper()
	index, err := NewBleveDiskIndex(
		b.Context(),
		root,
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		b.Fatal(err)
	}
	body := "economics markets policy trade " + strings.Repeat("bounded retrieval evidence ", 32)
	for first := 0; first < bleveReadMappingBenchmarkDocuments; first += 1_000 {
		batches := make([]*bleve.Batch, len(index.shards))
		for position, shard := range index.shards {
			batches[position] = shard.NewBatch()
		}
		for ordinal := first; ordinal < min(first+1_000, bleveReadMappingBenchmarkDocuments); ordinal++ {
			document := documentstore.Document{
				NormalizedURL: fmt.Sprintf("https://benchmark.example/%d", ordinal),
				Title:         fmt.Sprintf("Economics reference %d", ordinal),
				ExtractedText: body,
				Language:      "en",
			}
			indexed, encodeErr := bleveDocumentFromStore(document)
			if encodeErr != nil {
				b.Fatal(encodeErr)
			}
			shard := diskShardNumber(index.shards, document.NormalizedURL)
			if batchErr := batches[shard].Index(document.NormalizedURL, indexed); batchErr != nil {
				b.Fatal(batchErr)
			}
		}
		for position, shard := range index.shards {
			if batchErr := shard.Batch(batches[position]); batchErr != nil {
				b.Fatal(batchErr)
			}
		}
	}
	if err := index.Close(); err != nil {
		b.Fatal(err)
	}
	prepared, err := NewBleveDiskIndex(
		b.Context(),
		root,
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		b.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		b.Fatal(err)
	}
}

func openBleveReadMappingBenchmarkShards(b *testing.B, root string) []bleve.Index {
	b.Helper()
	shards := make([]bleve.Index, 0, diskShardCount)
	for position := range diskShardCount {
		shard, err := openBleveDisk(diskShardPath(root, position))
		if err != nil {
			closeBleveShards(shards)
			b.Fatal(err)
		}
		enableBM25Scoring(shard)
		shards = append(shards, shard)
	}

	return shards
}

func prepareBleveReadMappingBenchmarkFiles(ctx context.Context, shards []bleve.Index) error {
	window := make([]byte, bleveReadCacheWindowBytes)
	for _, shard := range shards {
		snapshot, err := (scorchBleveReadCacheShard{index: shard}).snapshot()
		if err != nil {
			return err
		}
		segments, err := snapshot.activeSegments()
		if err != nil {
			_ = snapshot.Close()

			return err
		}
		for _, segment := range segments {
			loaded, loadErr := loadBleveReadCacheSegment(
				ctx,
				segment.path,
				window,
				openBleveReadCacheSegment,
			)
			if loadErr != nil {
				_ = snapshot.Close()

				return loadErr
			}
			if loaded != uint64(len(segment.mapping)) {
				_ = snapshot.Close()

				return io.ErrUnexpectedEOF
			}
		}
		if err := snapshot.Close(); err != nil {
			return fmt.Errorf("close benchmark snapshot: %w", err)
		}
	}

	return nil
}
