package main

import (
	"context"
	"os"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestRunMigratesExistingStorage(t *testing.T) {
	t.Setenv(infrastructure.EnvDataDir, t.TempDir())

	storage, err := infrastructure.OpenBboltStorage(infrastructure.StoragePath(os.Getenv), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	word := yacymodel.WordHash("word")
	entry := yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:        yacymodel.WordHash("url").String(),
			yacymodel.ColLocalLinkCount: "1",
		},
	}
	if _, err := storage.AppendRWI(
		context.Background(),
		[]yacymodel.RWIPosting{entry},
	); err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
}
