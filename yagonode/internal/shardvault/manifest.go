package shardvault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const manifestName = "manifest.json"

// layoutManifest records the immutable shard layout so later opens (and any
// future reshard tool) agree on the routing.
type layoutManifest struct {
	Version int `json:"version"`
	Shards  int `json:"shards"`
}

// loadOrCreateManifest reads the layout manifest, writing a fresh one with
// the given shard count when the directory is new.
func loadOrCreateManifest(dir string, shards int) (layoutManifest, error) {
	path := filepath.Join(dir, manifestName)
	raw, err := os.ReadFile(path) //nolint:gosec // path derives from the configured storage dir.
	if err == nil {
		var manifest layoutManifest
		if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Shards <= 0 {
			return layoutManifest{}, fmt.Errorf("corrupt shard manifest at %s: %w", path, err)
		}

		return manifest, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return layoutManifest{}, fmt.Errorf("create storage directory: %w", err)
	}
	manifest := layoutManifest{Version: 1, Shards: shards}
	encoded := fmt.Appendf(nil, `{"version":%d,"shards":%d}`, manifest.Version, manifest.Shards)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return layoutManifest{}, fmt.Errorf("write shard manifest: %w", err)
	}

	return manifest, nil
}
