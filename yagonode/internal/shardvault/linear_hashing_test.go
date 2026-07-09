package shardvault

import (
	"os"
	"path/filepath"
	"testing"
)

// spread turns an index into a deterministic 64-bit hash across the space
// without a random source, so the routing tests are reproducible.
func spread(i uint64) uint64 { return i * 0x9E3779B97F4A7C15 }

// TestLocateSplitZeroMatchesModulo pins the behaviour-preserving guarantee: an
// unsplit pool (split == 0) routes exactly like the pre-ADR-0037 hash % 2^level,
// at every level, so adopting linear hashing moves no key in an existing vault.
func TestLocateSplitZeroMatchesModulo(t *testing.T) {
	for level := 1; level <= 10; level++ {
		e := &engine{level: level, split: 0}
		mask := uint64(1)<<level - 1
		for i := uint64(0); i < 20000; i++ {
			sum := spread(i)
			want := int(sum & mask) //nolint:gosec // sum&mask < 2^level.
			if got := e.locate(sum); got != want {
				t.Fatalf("level %d sum %d: locate=%d modulo=%d", level, sum, got, want)
			}
		}
	}
}

// TestLocateSplitPartition checks the split invariant at every intermediate
// pointer: a key in an already-split bucket lands in that bucket or its sibling
// bucket+2^level by the extra hash bit, an unsplit bucket's key stays put, and
// every index is in range.
func TestLocateSplitPartition(t *testing.T) {
	const level = 3
	for split := range 1 << level {
		e := &engine{level: level, split: split}
		count := 1<<level + split
		for i := uint64(0); i < 20000; i++ {
			sum := spread(i)
			idx := e.locate(sum)
			if idx < 0 || idx >= count {
				t.Fatalf("level %d split %d: locate=%d out of [0,%d)", level, split, idx, count)
			}
			low := int(sum & (1<<level - 1))
			switch {
			case low >= split:
				if idx != low {
					t.Fatalf("unsplit bucket key low=%d went to %d", low, idx)
				}
			case idx != low && idx != low+1<<level:
				t.Fatalf(
					"split bucket key %d went to %d, want %d or %d",
					low,
					idx,
					low,
					low+1<<level,
				)
			}
		}
	}
}

// TestLocateLevelRollIsRoutePreserving proves the pointer wrap (level, 2^level)
// → (level+1, 0) never re-routes a key, so a completed round of splits is a
// clean doubling — the correctness hinge the split pass relies on.
func TestLocateLevelRollIsRoutePreserving(t *testing.T) {
	for level := 1; level <= 9; level++ {
		full := &engine{level: level, split: 1 << level}
		rolled := &engine{level: level + 1, split: 0}
		for i := uint64(0); i < 20000; i++ {
			sum := spread(i)
			if full.locate(sum) != rolled.locate(sum) {
				t.Fatalf("level %d roll changed routing for sum %d", level, sum)
			}
		}
	}
}

func TestParseManifestVersions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		want    layoutManifest
		wantErr bool
	}{
		{"v1 power of two", `{"version":1,"shards":16}`, layoutManifest{Level: 4}, false},
		{"v1 floor", `{"version":1,"shards":8}`, layoutManifest{Level: 3}, false},
		{"v1 non power of two", `{"version":1,"shards":24}`, layoutManifest{}, true},
		{"v1 zero", `{"version":1,"shards":0}`, layoutManifest{}, true},
		{"v2 unsplit", `{"version":2,"level":3,"split":0}`, layoutManifest{Level: 3}, false},
		{"v2 split", `{"version":2,"level":4,"split":5}`, layoutManifest{Level: 4, Split: 5}, false},
		{"v2 split at boundary", `{"version":2,"level":4,"split":16}`, layoutManifest{}, true},
		{"v2 negative split", `{"version":2,"level":4,"split":-1}`, layoutManifest{}, true},
		{"unknown version", `{"version":9}`, layoutManifest{}, true},
		{"junk", `not json`, layoutManifest{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseManifest("m.json", []byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}

				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("parseManifest = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// TestManifestWriteReadRoundTrip writes a split state and reads it back: an
// existing manifest wins over the requested initial count and keeps its split.
func TestManifestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := layoutManifest{Level: 4, Split: 5}
	if err := writeManifest(dir, want); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	got, err := loadOrCreateManifest(dir, 8)
	if err != nil || got != want {
		t.Fatalf("reload = %+v, %v; want %+v", got, err, want)
	}
}

// TestOpenEngineReadsLegacyV1Manifest opens a real directory whose on-disk
// manifest is the pre-ADR-0037 version-1 form and confirms it converts to the
// equivalent linear-hashing state and opens the recorded number of shard files.
func TestOpenEngineReadsLegacyV1Manifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, manifestName),
		[]byte(`{"version":1,"shards":16}`),
		0o600,
	); err != nil {
		t.Fatalf("write v1 manifest: %v", err)
	}
	e, err := openEngine(dir, 1<<20)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if e.level != 4 || e.split != 0 || len(e.shards) != 16 {
		t.Fatalf(
			"legacy open = level %d split %d shards %d, want 4/0/16",
			e.level,
			e.split,
			len(e.shards),
		)
	}
}

func TestExactLog2(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want int
		ok   bool
	}{
		{1, 0, true},
		{2, 1, true},
		{8, 3, true},
		{1024, 10, true},
		{0, 0, false},
		{3, 0, false},
		{24, 0, false},
		{-4, 0, false},
	} {
		got, err := exactLog2(tc.n)
		if tc.ok && (err != nil || got != tc.want) {
			t.Fatalf("exactLog2(%d) = %d, %v; want %d", tc.n, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Fatalf("exactLog2(%d) should error", tc.n)
		}
	}
}

func TestLayoutShardCount(t *testing.T) {
	for _, tc := range []struct {
		m    layoutManifest
		want int
	}{
		{layoutManifest{Level: 3}, 8},
		{layoutManifest{Level: 3, Split: 5}, 13},
		{layoutManifest{Level: 4}, 16},
	} {
		if got := tc.m.shardCount(); got != tc.want {
			t.Fatalf("%+v shardCount=%d want %d", tc.m, got, tc.want)
		}
	}
}
