package shardvault

import (
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
)

const manifestName = "manifest.json"

// layoutManifest records the shard layout as a linear-hashing state so later
// opens and the split pass agree on the routing: the pool holds 2^Level + Split
// shard files, and a record's shard is chosen by hashing under that state
// (ADR-0037). Split is 0 for a pool that has not grown past a power-of-two
// boundary — the shape every pre-ADR-0037 vault is in.
type layoutManifest struct {
	Level int
	Split int
}

// shardCount is the number of shard files the state addresses.
func (m layoutManifest) shardCount() int {
	return 1<<m.Level + m.Split
}

// manifestDoc is the on-disk JSON shape across versions: version 1 recorded a
// flat power-of-two Shards count; version 2 records the linear-hashing Level
// and Split directly.
type manifestDoc struct {
	Version int `json:"version"`
	Shards  int `json:"shards"`
	Level   int `json:"level"`
	Split   int `json:"split"`
}

// loadOrCreateManifest reads the layout manifest, writing a fresh one at the
// given initial shard count (a power of two) when the directory is new.
func loadOrCreateManifest(dir string, initialShards int) (layoutManifest, error) {
	path := filepath.Join(dir, manifestName)
	raw, err := os.ReadFile(path) //nolint:gosec // path derives from the configured storage dir.
	if err == nil {
		return parseManifest(path, raw)
	}
	level, err := exactLog2(initialShards)
	if err != nil {
		return layoutManifest{}, fmt.Errorf("initial shard count: %w", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return layoutManifest{}, fmt.Errorf("create storage directory: %w", err)
	}
	manifest := layoutManifest{Level: level, Split: 0}
	if err := writeManifest(dir, manifest); err != nil {
		return layoutManifest{}, err
	}

	return manifest, nil
}

// parseManifest decodes a manifest file, converting a version-1 flat shard
// count to the equivalent linear-hashing state — a power-of-two N is state
// (log2 N, 0), so an existing vault adopts the scheme with no data migration.
func parseManifest(path string, raw []byte) (layoutManifest, error) {
	corrupt := func(detail string) error {
		return fmt.Errorf("corrupt shard manifest at %s: %s", path, detail)
	}
	var doc manifestDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return layoutManifest{}, fmt.Errorf("corrupt shard manifest at %s: %w", path, err)
	}
	switch doc.Version {
	case 1:
		level, err := exactLog2(doc.Shards)
		if err != nil {
			return layoutManifest{}, corrupt(err.Error())
		}

		return layoutManifest{Level: level, Split: 0}, nil
	case 2:
		if doc.Level < 0 || doc.Split < 0 || doc.Split >= 1<<doc.Level {
			return layoutManifest{}, corrupt(fmt.Sprintf("level %d split %d", doc.Level, doc.Split))
		}

		return layoutManifest{Level: doc.Level, Split: doc.Split}, nil
	default:
		return layoutManifest{}, corrupt(fmt.Sprintf("unknown version %d", doc.Version))
	}
}

// writeManifest persists the layout atomically — a temp file renamed over the
// manifest — so a split's flip is crash-atomic and a torn write never leaves a
// half-written manifest.
func writeManifest(dir string, m layoutManifest) error {
	path := filepath.Join(dir, manifestName)
	tmp := path + ".tmp"
	encoded := fmt.Appendf(nil, `{"version":2,"level":%d,"split":%d}`, m.Level, m.Split)
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return fmt.Errorf("write shard manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("commit shard manifest: %w", err)
	}

	return nil
}

// exactLog2 returns log2(n) for a power of two, erroring otherwise so a corrupt
// or non-power-of-two shard count is rejected rather than silently re-routing
// every key under the linear-hashing masks.
func exactLog2(n int) (int, error) {
	if n <= 0 || n&(n-1) != 0 {
		return 0, fmt.Errorf("shard count %d is not a positive power of two", n)
	}

	return bits.TrailingZeros(uint(n)), nil
}
