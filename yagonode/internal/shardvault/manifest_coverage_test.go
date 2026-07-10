package shardvault

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreateManifestNonPowerOfTwo(t *testing.T) {
	_, err := loadOrCreateManifest(filepath.Join(t.TempDir(), "fresh"), 7)
	assertErr(t, err, "non-power-of-two initial shards")
}

func TestWriteManifestRenameError(t *testing.T) {
	dir := t.TempDir()
	mustNonEmptyDir(t, filepath.Join(dir, manifestName))
	assertErr(t, writeManifest(dir, layoutManifest{Level: 3}), "commit shard manifest")
}
