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
	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: yacymodel.WordHash("url").String(),
		},
	}
	if _, err := storage.StoreURLs(
		context.Background(),
		[]yacymodel.URIMetadataRow{row},
	); err != nil {
		t.Fatalf("StoreURLs: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
}
