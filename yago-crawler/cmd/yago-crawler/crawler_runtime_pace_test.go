package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func TestAssembleCrawlerPacePropagatesCheckpointReadFailure(t *testing.T) {
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier.db"))
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	if _, err := assembleCrawlerPace(
		t.Context(),
		DefaultCrawlConfig(),
		checkpoint,
		nil,
	); !errors.Is(err, frontiercheckpoint.ErrClosed) {
		t.Fatalf("assemble crawler pace error = %v", err)
	}
}
