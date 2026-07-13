package urlmeta

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestPurgeNotifiesObserverOfDeletedURLs(t *testing.T) {
	ctx := context.Background()
	observer := &recordingObserver{}
	v, module := openObservedModule(t, observer)
	row := urlRow(t, "a")
	if _, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		if _, purgeErr := module.Evictor.Purge(
			ctx,
			tx,
			[]yagomodel.Hash{rowHash(t, row)},
		); purgeErr != nil {
			return fmt.Errorf("purge: %w", purgeErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(observer.purged) != 1 || observer.purged[0] != rowHash(t, row) {
		t.Fatalf("purged = %v, want one matching hash", observer.purged)
	}
}

func TestPurgeSurvivesObserverFailure(t *testing.T) {
	ctx := context.Background()
	observer := &recordingObserver{fail: true}
	v, module := openObservedModule(t, observer)
	row := urlRow(t, "a")
	if _, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	var result PurgeResult
	if err := v.Update(ctx, func(tx *vault.Txn) error {
		purged, purgeErr := module.Evictor.Purge(ctx, tx, []yagomodel.Hash{rowHash(t, row)})
		result = purged
		if purgeErr != nil {
			return fmt.Errorf("purge: %w", purgeErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.URLsDeleted != 1 {
		t.Fatalf("URLsDeleted = %d, want 1 despite observer failure", result.URLsDeleted)
	}
}

func TestPurgeLogsObserverFailureOnceAfterReplay(t *testing.T) {
	observer := &recordingObserver{}
	storage, module, engine := openScriptedModule(t, observer)
	row := urlRow(t, "purge-replay")
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{row},
	); err != nil {
		t.Fatal(err)
	}
	observer.fail = true
	engine.replayNext = true
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var result PurgeResult
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		var err error
		result, err = module.Evictor.Purge(
			t.Context(),
			tx,
			[]yagomodel.Hash{rowHash(t, row)},
		)
		if err != nil {
			return fmt.Errorf("purge URL row: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result.ReportObserverFailures(t.Context())
	if count := strings.Count(output.String(), urlObserverFailed); count != 1 {
		t.Fatalf("observer logs = %d, want one: %s", count, output.String())
	}
}

func TestPurgeDeletesRows(t *testing.T) {
	ctx := context.Background()
	v, module := openObservedModule(t)
	row := urlRow(t, "a")
	if _, err := module.Receiver.Receive(
		ctx,
		[]yagomodel.URIMetadataRow{row, urlRow(t, "b")},
	); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	var result PurgeResult
	if err := v.Update(ctx, func(tx *vault.Txn) error {
		purged, purgeErr := module.Evictor.Purge(ctx, tx, []yagomodel.Hash{rowHash(t, row)})
		result = purged
		if purgeErr != nil {
			return fmt.Errorf("purge: %w", purgeErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.URLsDeleted != 1 {
		t.Fatalf("URLsDeleted = %d, want 1", result.URLsDeleted)
	}

	count, err := module.Directory.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Count = %d, want 1", count)
	}
}
