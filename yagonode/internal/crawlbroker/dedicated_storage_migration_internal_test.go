package crawlbroker

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLegacyStorageVersionOneBucketsRemainFrozen(t *testing.T) {
	want := []vault.Name{
		orderBucket,
		normalOrderIndexBucket,
		automaticOrderIndexBucket,
		seqBucket,
		idempotencyBucket,
		leaseBucket,
		leaseSettlementBucket,
		leaseSettlementOrderBucket,
		leaseSettlementExpiryBucket,
		leaseControlTargetBucket,
		completedLeaseControlTargetBucket,
		terminalSettlementSecretBucket,
		controlDirectiveBucket,
		controlDirectiveState,
	}
	got := legacyStorageVersionOneBuckets()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("dedicated storage buckets = %v, want %v", got, want)
	}
	for index := 1; index < len(got); index++ {
		if got[index] == got[index-1] {
			t.Fatalf("duplicate dedicated storage bucket %q", got[index])
		}
	}
}

func TestLegacyStorageMigrationWrapsVaultFailure(t *testing.T) {
	err := MigrateLegacyStorage(t.Context(), nil, nil)
	if err == nil || !strings.Contains(
		err.Error(),
		"migrate legacy crawl broker storage: invalid retained bucket migration",
	) {
		t.Fatalf("migration failure = %v", err)
	}
}

func TestLegacyStorageMigrationResumesCommittedBoltPageAfterReopen(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "legacy.db")
	source := openMigrationTestBroker(t, sourcePath)
	t.Cleanup(func() { closeMigrationTestBroker(source) })
	publishMigrationTestOrders(t, ctx, source.broker)
	path := filepath.Join(root, "crawlbroker.db")
	commitInterruptedMigrationPage(t, ctx, source, path)
	target, err := boltvault.OpenWithLockTimeout(path, crawlRuntimeMigrationTestTimeout)
	if err != nil {
		t.Fatalf("reopen interrupted target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	if err := MigrateLegacyStorage(ctx, source.storage, target); err != nil {
		t.Fatalf("resume legacy migration: %v", err)
	}
	assertMigratedTarget(t, ctx, target)

	source.broker.Close()
	if err := source.storage.Close(); err != nil {
		t.Fatalf("close legacy source: %v", err)
	}
	assertRetainedMigrationSource(t, ctx, sourcePath)
}

type migrationTestBroker struct {
	storage *vault.Vault
	broker  *CrawlBroker
}

type migrationTestRow struct {
	key   vault.Key
	value []byte
}

func openMigrationTestBroker(t *testing.T, path string) migrationTestBroker {
	t.Helper()
	storage, err := boltvault.OpenWithLockTimeout(path, crawlRuntimeMigrationTestTimeout)
	if err != nil {
		t.Fatalf("open migration storage: %v", err)
	}
	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, storage, nil)
	if err != nil {
		_ = storage.Close()
		t.Fatalf("open migration broker: %v", err)
	}

	return migrationTestBroker{storage: storage, broker: broker}
}

func closeMigrationTestBroker(runtime migrationTestBroker) {
	runtime.broker.Close()
	_ = runtime.storage.Close()
}

func publishMigrationTestOrders(
	t *testing.T,
	ctx context.Context,
	broker *CrawlBroker,
) {
	t.Helper()
	for index := range retainedBucketMigrationTestRows {
		order := yagocrawlcontract.CrawlOrder{
			Provenance: []byte("migration"),
			Profile: yagocrawlcontract.NewCrawlProfile(
				yagocrawlcontract.CrawlProfile{Name: fmt.Sprintf("profile-%03d", index)},
			),
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL: fmt.Sprintf("https://example.org/%03d", index),
			}},
		}
		duplicate, err := broker.Orders.PublishOnce(
			ctx,
			fmt.Sprintf("migration-%03d", index),
			order,
		)
		if err != nil || duplicate {
			t.Fatalf("publish source row %d: duplicate=%t error=%v", index, duplicate, err)
		}
	}
}

func commitInterruptedMigrationPage(
	t *testing.T,
	ctx context.Context,
	source migrationTestBroker,
	path string,
) {
	t.Helper()
	target, err := boltvault.OpenWithLockTimeout(path, crawlRuntimeMigrationTestTimeout)
	if err != nil {
		t.Fatalf("open interrupted target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	targetOrders, err := vault.Register(target, orderBucket, orderCodec{})
	if err != nil {
		t.Fatalf("register target orders: %v", err)
	}
	targetMarker, err := vault.Register(target, legacyStorageMigrationMarker, orderCodec{})
	if err != nil {
		t.Fatalf("register target marker: %v", err)
	}
	page := readMigrationTestPage(t, ctx, source)
	if err := target.Update(ctx, func(tx *vault.Txn) error {
		for _, entry := range page {
			if err := targetOrders.Put(tx, entry.key, entry.value); err != nil {
				return fmt.Errorf("store interrupted target order: %w", err)
			}
		}
		if err := targetMarker.Put(
			tx,
			vault.Key("cursor:"+orderBucket),
			page[len(page)-1].key,
		); err != nil {
			return fmt.Errorf("store interrupted migration cursor: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("commit interrupted migration page: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close interrupted target: %v", err)
	}
}

func readMigrationTestPage(
	t *testing.T,
	ctx context.Context,
	source migrationTestBroker,
) []migrationTestRow {
	t.Helper()
	page := make([]migrationTestRow, 0, retainedBucketMigrationCommittedRows)
	if err := source.storage.View(ctx, func(tx *vault.Txn) error {
		if err := source.broker.Orders.orders.Scan(
			tx,
			nil,
			func(key vault.Key, value []byte) (bool, error) {
				page = append(page, migrationTestRow{
					key:   append(vault.Key(nil), key...),
					value: append([]byte(nil), value...),
				})

				return len(page) < retainedBucketMigrationCommittedRows, nil
			},
		); err != nil {
			return fmt.Errorf("scan source migration orders: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read committed page: %v", err)
	}
	if len(page) != retainedBucketMigrationCommittedRows {
		t.Fatalf(
			"committed page rows = %d, want %d",
			len(page),
			retainedBucketMigrationCommittedRows,
		)
	}

	return page
}

func assertMigratedTarget(t *testing.T, ctx context.Context, target *vault.Vault) {
	t.Helper()
	targetBroker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, target, nil)
	if err != nil {
		t.Fatalf("open migrated broker: %v", err)
	}
	t.Cleanup(targetBroker.Close)
	depth, err := targetBroker.Orders.Depth(ctx)
	if err != nil || depth.Pending != retainedBucketMigrationTestRows {
		t.Fatalf("migrated depth = %+v, error=%v", depth, err)
	}
	duplicate, err := targetBroker.Orders.PublishOnce(
		ctx,
		"migration-299",
		yagocrawlcontract.CrawlOrder{},
	)
	if err != nil || !duplicate {
		t.Fatalf("migrated idempotency: duplicate=%t error=%v", duplicate, err)
	}
	assertMigrationOrderSequence(t, ctx, targetBroker)
}

func assertMigrationOrderSequence(t *testing.T, ctx context.Context, broker *CrawlBroker) {
	t.Helper()
	for index := range retainedBucketMigrationTestRows {
		encoded, _, found, leaseErr := broker.Orders.leasePop(ctx, "target-worker")
		if leaseErr != nil || !found {
			t.Fatalf("lease migrated row %d: found=%t error=%v", index, found, leaseErr)
		}
		order, decodeErr := yagocrawlcontract.UnmarshalCrawlOrder(encoded)
		if decodeErr != nil {
			t.Fatalf("decode migrated row %d: %v", index, decodeErr)
		}
		want := fmt.Sprintf("profile-%03d", index)
		if order.Profile.Name != want {
			t.Fatalf("migrated row %d profile = %q, want %q", index, order.Profile.Name, want)
		}
	}
}

func assertRetainedMigrationSource(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	source := openMigrationTestBroker(t, path)
	t.Cleanup(func() { closeMigrationTestBroker(source) })
	sourceDepth, err := source.broker.Orders.Depth(ctx)
	if err != nil || sourceDepth.Pending != retainedBucketMigrationTestRows ||
		sourceDepth.Leased != 0 {
		t.Fatalf("retained source depth = %+v, error=%v", sourceDepth, err)
	}
}

const (
	retainedBucketMigrationTestRows      = 300
	retainedBucketMigrationCommittedRows = 256
	crawlRuntimeMigrationTestTimeout     = 100 * time.Millisecond
)
