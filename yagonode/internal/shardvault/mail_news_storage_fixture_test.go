package shardvault

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	mailNewsMailboxBucket   vault.Name = "peermessage"
	mailNewsRecoveryBucket  vault.Name = "mail-news-recovery"
	mailNewsKnownBucket     vault.Name = "peernews-known"
	mailNewsCategoryBucket  vault.Name = "peernews-known-category"
	mailNewsQueueBucket     vault.Name = "peernews-queue"
	mailNewsCursorBucket    vault.Name = "peernews-cursor"
	mailNewsMaximumWireSize            = 4096
)

type mailNewsWireRow struct {
	Identity string `json:"identity"`
	Payload  string `json:"payload"`
	Sequence uint64 `json:"sequence"`
}

type mailNewsSweepFixture struct {
	name    string
	bucket  vault.Name
	records int
	page    int
}

type mailNewsSweepCursor struct {
	After string `json:"after"`
}

type mailNewsMailboxAdmissionIntent struct {
	Previous string          `json:"previous"`
	Accepted string          `json:"accepted"`
	Row      mailNewsWireRow `json:"row"`
}

type mailNewsRecordAdmissionIntent struct {
	Identity string          `json:"identity"`
	QueueKey string          `json:"queue_key"`
	Cursor   string          `json:"cursor"`
	Row      mailNewsWireRow `json:"row"`
}

type mailNewsRotationIntent struct {
	Source string          `json:"source"`
	Target string          `json:"target"`
	Row    mailNewsWireRow `json:"row"`
}

type mailNewsCursorExpectation struct {
	queue    string
	sequence uint64
	row      vault.Key
	shard    int
}

type mailNewsJSONCodec[V any] struct{}

type mailNewsSweepStorage struct {
	bucket vault.Name
	rows   *vault.Keyspace[mailNewsWireRow]
	cursor *vault.Keyspace[mailNewsSweepCursor]
}

type mailNewsMailboxFixture struct {
	rows     *vault.Keyspace[mailNewsWireRow]
	recovery *vault.Keyspace[mailNewsMailboxAdmissionIntent]
}

type mailNewsRecordFixture struct {
	known      *vault.Keyspace[string]
	categories *vault.Keyspace[string]
	queue      *vault.Keyspace[mailNewsWireRow]
	cursors    *vault.Keyspace[uint64]
	admission  *vault.Keyspace[mailNewsRecordAdmissionIntent]
	rotation   *vault.Keyspace[mailNewsRotationIntent]
}

func (mailNewsJSONCodec[V]) Encode(value V) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode mail/news fixture: %w", err)
	}

	return raw, nil
}

func (mailNewsJSONCodec[V]) Decode(raw []byte) (V, error) {
	var value V
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode mail/news fixture: %w", err)
	}

	return value, nil
}

func mailNewsOpenStorage(t *testing.T, directory string) (*engine, *vault.Vault) {
	t.Helper()
	shards, err := openEngine(directory, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	return shards, storage
}

func mailNewsCloseStorage(t *testing.T, storage *vault.Vault) {
	t.Helper()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
}

func mailNewsRegisterSweepFixture(
	t *testing.T,
	storage *vault.Vault,
	bucket vault.Name,
) mailNewsSweepStorage {
	t.Helper()
	rows, err := vault.RegisterKeyspace[mailNewsWireRow](
		storage, bucket, mailNewsJSONCodec[mailNewsWireRow]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := vault.RegisterKeyspace[mailNewsSweepCursor](
		storage, mailNewsRecoveryBucket, mailNewsJSONCodec[mailNewsSweepCursor]{},
	)
	if err != nil {
		t.Fatal(err)
	}

	return mailNewsSweepStorage{bucket: bucket, rows: rows, cursor: cursor}
}

func mailNewsRegisterMailboxFixture(
	t *testing.T,
	storage *vault.Vault,
) mailNewsMailboxFixture {
	t.Helper()
	rows, err := vault.RegisterKeyspace[mailNewsWireRow](
		storage, mailNewsMailboxBucket, mailNewsJSONCodec[mailNewsWireRow]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := vault.RegisterKeyspace[mailNewsMailboxAdmissionIntent](
		storage, mailNewsRecoveryBucket, mailNewsJSONCodec[mailNewsMailboxAdmissionIntent]{},
	)
	if err != nil {
		t.Fatal(err)
	}

	return mailNewsMailboxFixture{rows: rows, recovery: recovery}
}

func mailNewsRegisterRecordFixture(
	t *testing.T,
	storage *vault.Vault,
) mailNewsRecordFixture {
	t.Helper()
	known := mailNewsRegisterKeyspace[string](t, storage, mailNewsKnownBucket)
	categories := mailNewsRegisterKeyspace[string](t, storage, mailNewsCategoryBucket)
	queue := mailNewsRegisterKeyspace[mailNewsWireRow](t, storage, mailNewsQueueBucket)
	cursors := mailNewsRegisterKeyspace[uint64](t, storage, mailNewsCursorBucket)
	admission := mailNewsRegisterKeyspace[mailNewsRecordAdmissionIntent](
		t, storage, mailNewsRecoveryBucket+"-admission",
	)
	rotation := mailNewsRegisterKeyspace[mailNewsRotationIntent](
		t, storage, mailNewsRecoveryBucket+"-rotation",
	)

	return mailNewsRecordFixture{
		known: known, categories: categories, queue: queue, cursors: cursors,
		admission: admission, rotation: rotation,
	}
}

func mailNewsRegisterKeyspace[V any](
	t *testing.T,
	storage *vault.Vault,
	bucket vault.Name,
) *vault.Keyspace[V] {
	t.Helper()
	space, err := vault.RegisterKeyspace[V](storage, bucket, mailNewsJSONCodec[V]{})
	if err != nil {
		t.Fatal(err)
	}

	return space
}

func mailNewsSeedSweepRows(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsSweepStorage,
	records int,
) {
	t.Helper()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range records {
			key := vault.Key(fmt.Sprintf("row-%08d", index))
			row := mailNewsWireRow{
				Identity: string(key), Payload: "bounded fixture", Sequence: uint64(index + 1),
			}
			if err := fixture.rows.Put(tx, key, row); err != nil {
				return fmt.Errorf("seed mail/news row: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mailNewsSweepRows(
	storage *vault.Vault,
	fixture mailNewsSweepStorage,
	pageSize int,
) (vault.Key, error) {
	after, err := mailNewsLoadSweepCursor(storage, fixture.cursor)
	if err != nil {
		return nil, err
	}
	var firstPageAfter vault.Key
	firstPageObserved := false
	for {
		if !firstPageObserved {
			firstPageAfter = append(vault.Key(nil), after...)
			firstPageObserved = true
		}
		page, err := mailNewsReadKeyPage(storage, fixture.bucket, after, pageSize)
		if err != nil {
			return firstPageAfter, err
		}
		if err := mailNewsValidateSweepPage(storage, fixture.rows, page.Keys); err != nil {
			return firstPageAfter, err
		}
		if len(page.Keys) == 0 || !page.More {
			return firstPageAfter, mailNewsClearSweepCursor(storage, fixture.cursor)
		}
		after = page.Keys[len(page.Keys)-1]
		if err := mailNewsStoreSweepCursor(storage, fixture.cursor, after); err != nil {
			return firstPageAfter, err
		}
	}
}

func mailNewsLoadSweepCursor(
	storage *vault.Vault,
	cursor *vault.Keyspace[mailNewsSweepCursor],
) (vault.Key, error) {
	var after vault.Key
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		value, found, err := cursor.Get(tx, vault.Key("scrub"))
		if err != nil {
			return fmt.Errorf("read mail/news sweep cursor: %w", err)
		}
		if found {
			after = vault.Key(value.After)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load mail/news sweep cursor: %w", err)
	}

	return after, nil
}

func mailNewsReadKeyPage(
	storage *vault.Vault,
	bucket vault.Name,
	after vault.Key,
	pageSize int,
) (vault.BucketKeyPage, error) {
	var page vault.BucketKeyPage
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		page, err = tx.ReadBucketKeyPage(bucket, after, pageSize)
		if err != nil {
			return fmt.Errorf("read mail/news key page: %w", err)
		}

		return nil
	}); err != nil {
		return vault.BucketKeyPage{}, fmt.Errorf("scan mail/news key page: %w", err)
	}

	return page, nil
}

func mailNewsValidateSweepPage(
	storage *vault.Vault,
	rows *vault.Keyspace[mailNewsWireRow],
	keys []vault.Key,
) error {
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		for _, key := range keys {
			size, found, err := rows.EncodedSize(tx, key)
			if err != nil {
				return fmt.Errorf("inspect mail/news row size: %w", err)
			}
			if !found || size > mailNewsMaximumWireSize {
				return fmt.Errorf("mail/news row %q has invalid size %d", key, size)
			}
			row, found, err := rows.Get(tx, key)
			if err != nil {
				return fmt.Errorf("decode mail/news row: %w", err)
			}
			if !found || row.Identity == "" {
				return fmt.Errorf("mail/news row %q is incomplete", key)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("validate mail/news page: %w", err)
	}

	return nil
}

func mailNewsStoreSweepCursor(
	storage *vault.Vault,
	cursor *vault.Keyspace[mailNewsSweepCursor],
	after vault.Key,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if err := cursor.Put(
			tx,
			vault.Key("scrub"),
			mailNewsSweepCursor{After: string(after)},
		); err != nil {
			return fmt.Errorf("store mail/news sweep cursor: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("commit mail/news sweep cursor: %w", err)
	}

	return nil
}

func mailNewsClearSweepCursor(
	storage *vault.Vault,
	cursor *vault.Keyspace[mailNewsSweepCursor],
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := cursor.Delete(tx, vault.Key("scrub")); err != nil {
			return fmt.Errorf("delete mail/news sweep cursor: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("clear mail/news sweep cursor: %w", err)
	}

	return nil
}

func mailNewsSeedMailbox(
	t *testing.T,
	engine *engine,
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
	order string,
) mailNewsMailboxAdmissionIntent {
	t.Helper()
	previous := mailNewsFindMiddleShardKey(t, engine, mailNewsMailboxBucket, "previous")
	previousShard := engine.route(mailNewsMailboxBucket, previous)
	accepted := mailNewsFindRelatedShardKey(
		t, engine, mailNewsMailboxBucket, previousShard, order == "accepted-first",
	)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.rows.Put(
			tx,
			previous,
			mailNewsWireRow{Identity: string(previous)},
		); err != nil {
			return fmt.Errorf("seed previous mailbox row: %w", err)
		}
		for index := 1; index < 1024; index++ {
			key := vault.Key(fmt.Sprintf("mailbox-%08d", index))
			if err := fixture.rows.Put(
				tx,
				key,
				mailNewsWireRow{Identity: string(key)},
			); err != nil {
				return fmt.Errorf("seed retained mailbox row: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return mailNewsMailboxAdmissionIntent{
		Previous: string(previous), Accepted: string(accepted),
		Row: mailNewsWireRow{Identity: string(accepted), Payload: order, Sequence: 1025},
	}
}

func mailNewsFindMiddleShardKey(
	t *testing.T,
	engine *engine,
	bucket vault.Name,
	label string,
) vault.Key {
	t.Helper()
	for candidate := range 100_000 {
		key := vault.Key(fmt.Sprintf("%s-%08d", label, candidate))
		shard := engine.route(bucket, key)
		if shard > 0 && shard < len(engine.shards)-1 {
			return key
		}
	}
	t.Fatal("no middle-shard fixture key found")

	return nil
}

func mailNewsFindRelatedShardKey(
	t *testing.T,
	engine *engine,
	bucket vault.Name,
	reference int,
	before bool,
) vault.Key {
	t.Helper()
	for candidate := range 100_000 {
		key := vault.Key(fmt.Sprintf("accepted-%08d", candidate))
		shard := engine.route(bucket, key)
		if before && shard < reference || !before && shard > reference {
			return key
		}
	}
	t.Fatal("no related-shard fixture key found")

	return nil
}

func mailNewsPersistMailboxAdmission(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
	intent mailNewsMailboxAdmissionIntent,
) {
	t.Helper()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.recovery.Put(tx, vault.Key("admission"), intent); err != nil {
			return fmt.Errorf("store mailbox admission intent: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mailNewsApplyMailboxAdmission(
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
	intent mailNewsMailboxAdmissionIntent,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := fixture.rows.Delete(tx, vault.Key(intent.Previous)); err != nil {
			return fmt.Errorf("delete previous mailbox row: %w", err)
		}
		if err := fixture.rows.Put(tx, vault.Key(intent.Accepted), intent.Row); err != nil {
			return fmt.Errorf("store accepted mailbox row: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("apply mailbox admission: %w", err)
	}

	return nil
}

func mailNewsRecoverMailboxAdmission(
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
) error {
	intent, found, err := mailNewsLoadMailboxAdmission(storage, fixture)
	if err != nil || !found {
		return err
	}
	if err := mailNewsApplyMailboxAdmission(storage, fixture, intent); err != nil {
		return err
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := fixture.recovery.Delete(tx, vault.Key("admission")); err != nil {
			return fmt.Errorf("delete mailbox admission intent: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("complete mailbox admission recovery: %w", err)
	}

	return nil
}

func mailNewsLoadMailboxAdmission(
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
) (mailNewsMailboxAdmissionIntent, bool, error) {
	var intent mailNewsMailboxAdmissionIntent
	var found bool
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		intent, found, err = fixture.recovery.Get(tx, vault.Key("admission"))
		if err != nil {
			return fmt.Errorf("read mailbox admission intent: %w", err)
		}

		return nil
	}); err != nil {
		return mailNewsMailboxAdmissionIntent{}, false, fmt.Errorf(
			"load mailbox admission intent: %w", err,
		)
	}

	return intent, found, nil
}

func mailNewsFindRecordAdmissionIntent(
	t *testing.T,
	engine *engine,
) mailNewsRecordAdmissionIntent {
	t.Helper()
	for candidate := range 100_000 {
		identity := fmt.Sprintf("news-%08d", candidate)
		queueKey := mailNewsQueueKey("incoming", uint64(candidate+1))
		intent := mailNewsRecordAdmissionIntent{
			Identity: identity,
			QueueKey: string(queueKey),
			Cursor:   "incoming",
			Row: mailNewsWireRow{
				Identity: identity,
				Payload:  "record",
				Sequence: uint64(candidate + 1),
			},
		}
		if len(mailNewsRecordAdmissionRoutes(engine, intent)) >= 2 {
			return intent
		}
	}
	t.Fatal("no cross-shard record admission fixture found")

	return mailNewsRecordAdmissionIntent{}
}

func mailNewsRecordAdmissionRoutes(
	engine *engine,
	intent mailNewsRecordAdmissionIntent,
) []int {
	unique := map[int]struct{}{
		engine.route(mailNewsKnownBucket, vault.Key(intent.Identity)):    {},
		engine.route(mailNewsCategoryBucket, vault.Key(intent.Identity)): {},
		engine.route(mailNewsQueueBucket, vault.Key(intent.QueueKey)):    {},
		engine.route(mailNewsCursorBucket, vault.Key(intent.Cursor)):     {},
	}
	routes := make([]int, 0, len(unique))
	for route := range unique {
		routes = append(routes, route)
	}
	slices.Sort(routes)

	return routes
}

func mailNewsPersistRecordAdmission(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	intent mailNewsRecordAdmissionIntent,
) {
	t.Helper()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.admission.Put(tx, vault.Key("admission"), intent); err != nil {
			return fmt.Errorf("store record admission intent: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mailNewsApplyRecordAdmission(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	intent mailNewsRecordAdmissionIntent,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if err := fixture.known.Put(tx, vault.Key(intent.Identity), "1"); err != nil {
			return fmt.Errorf("store known record marker: %w", err)
		}
		if err := fixture.categories.Put(tx, vault.Key(intent.Identity), "crawlstart"); err != nil {
			return fmt.Errorf("store record category: %w", err)
		}
		if err := fixture.queue.Put(tx, vault.Key(intent.QueueKey), intent.Row); err != nil {
			return fmt.Errorf("store queued record: %w", err)
		}
		if err := fixture.cursors.Put(
			tx,
			vault.Key(intent.Cursor),
			intent.Row.Sequence,
		); err != nil {
			return fmt.Errorf("store record cursor: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("apply record admission: %w", err)
	}

	return nil
}

func mailNewsRecoverRecordAdmission(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) error {
	intent, found, err := mailNewsLoadRecordAdmission(storage, fixture)
	if err != nil || !found {
		return err
	}
	if err := mailNewsApplyRecordAdmission(storage, fixture, intent); err != nil {
		return err
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := fixture.admission.Delete(tx, vault.Key("admission")); err != nil {
			return fmt.Errorf("delete record admission intent: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("complete record admission recovery: %w", err)
	}

	return nil
}

func mailNewsLoadRecordAdmission(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) (mailNewsRecordAdmissionIntent, bool, error) {
	var intent mailNewsRecordAdmissionIntent
	var found bool
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		intent, found, err = fixture.admission.Get(tx, vault.Key("admission"))
		if err != nil {
			return fmt.Errorf("read record admission intent: %w", err)
		}

		return nil
	}); err != nil {
		return mailNewsRecordAdmissionIntent{}, false, fmt.Errorf(
			"load record admission intent: %w", err,
		)
	}

	return intent, found, nil
}

func mailNewsRecordAdmissionPresence(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	intent mailNewsRecordAdmissionIntent,
) int {
	t.Helper()
	present := 0
	if _, found := mailNewsReadValue(t, storage, fixture.known, vault.Key(intent.Identity)); found {
		present++
	}
	if _, found := mailNewsReadValue(
		t,
		storage,
		fixture.categories,
		vault.Key(intent.Identity),
	); found {
		present++
	}
	if _, found := mailNewsReadValue(t, storage, fixture.queue, vault.Key(intent.QueueKey)); found {
		present++
	}
	if _, found := mailNewsReadValue(t, storage, fixture.cursors, vault.Key(intent.Cursor)); found {
		present++
	}

	return present
}

func mailNewsAssertRecordAdmissionRecovered(
	t *testing.T,
	directory string,
	intent mailNewsRecordAdmissionIntent,
) {
	t.Helper()
	engine, storage := mailNewsOpenStorage(t, directory)
	t.Cleanup(func() { _ = storage.Close() })
	fixture := mailNewsRegisterRecordFixture(t, storage)
	if err := mailNewsRecoverRecordAdmission(storage, fixture); err != nil {
		t.Fatal(err)
	}
	if present := mailNewsRecordAdmissionPresence(t, storage, fixture, intent); present != 4 {
		t.Fatalf("recovered record admission retained %d of 4 values", present)
	}
	for _, bucket := range []vault.Name{
		mailNewsKnownBucket, mailNewsCategoryBucket, mailNewsQueueBucket, mailNewsCursorBucket,
	} {
		if got := mailNewsBucketRows(t, engine, bucket); got != 1 {
			t.Fatalf("recovered bucket %s rows = %d, want 1", bucket, got)
		}
	}
	if _, found := mailNewsReadValue(t, storage, fixture.admission, vault.Key("admission")); found {
		t.Fatal("recovered record admission retained intent")
	}
	if err := mailNewsRecoverRecordAdmission(storage, fixture); err != nil {
		t.Fatal(fmt.Errorf("repeat record admission recovery: %w", err))
	}
}

func mailNewsFindRotationIntent(t *testing.T, engine *engine) mailNewsRotationIntent {
	t.Helper()
	for sequence := uint64(1); sequence < 100_000; sequence++ {
		source := mailNewsQueueKey("outgoing", sequence)
		target := mailNewsQueueKey("published", sequence+1)
		if engine.route(mailNewsQueueBucket, source) == engine.route(mailNewsQueueBucket, target) {
			continue
		}

		return mailNewsRotationIntent{
			Source: string(source), Target: string(target),
			Row: mailNewsWireRow{Identity: "rotation", Payload: "wire", Sequence: sequence},
		}
	}
	t.Fatal("no cross-shard rotation fixture found")

	return mailNewsRotationIntent{}
}

func mailNewsSeedRotation(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	intent mailNewsRotationIntent,
) {
	t.Helper()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.Put(tx, vault.Key(intent.Source), intent.Row); err != nil {
			return fmt.Errorf("seed rotation source: %w", err)
		}
		if err := fixture.rotation.Put(tx, vault.Key("rotation"), intent); err != nil {
			return fmt.Errorf("store rotation intent: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mailNewsApplyRotation(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	intent mailNewsRotationIntent,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := fixture.queue.Delete(tx, vault.Key(intent.Source)); err != nil {
			return fmt.Errorf("delete rotation source: %w", err)
		}
		if err := fixture.queue.Put(tx, vault.Key(intent.Target), intent.Row); err != nil {
			return fmt.Errorf("store rotation target: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("apply rotation: %w", err)
	}

	return nil
}

func mailNewsRecoverRotation(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) error {
	intent, found, err := mailNewsLoadRotation(storage, fixture)
	if err != nil || !found {
		return err
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if err := fixture.queue.Put(tx, vault.Key(intent.Source), intent.Row); err != nil {
			return fmt.Errorf("restore rotation source: %w", err)
		}
		if _, err := fixture.queue.Delete(tx, vault.Key(intent.Target)); err != nil {
			return fmt.Errorf("remove rotation target: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("recover rotation data: %w", err)
	}

	return mailNewsClearRotationIntent(storage, fixture)
}

func mailNewsLoadRotation(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) (mailNewsRotationIntent, bool, error) {
	var intent mailNewsRotationIntent
	var found bool
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		intent, found, err = fixture.rotation.Get(tx, vault.Key("rotation"))
		if err != nil {
			return fmt.Errorf("read rotation intent: %w", err)
		}

		return nil
	}); err != nil {
		return mailNewsRotationIntent{}, false, fmt.Errorf("load rotation intent: %w", err)
	}

	return intent, found, nil
}

func mailNewsClearRotationIntent(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		if _, err := fixture.rotation.Delete(tx, vault.Key("rotation")); err != nil {
			return fmt.Errorf("delete rotation intent: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("complete rotation recovery: %w", err)
	}

	return nil
}

func mailNewsAssertRotationRecovered(
	t *testing.T,
	directory string,
	intent mailNewsRotationIntent,
) {
	t.Helper()
	_, storage := mailNewsOpenStorage(t, directory)
	t.Cleanup(func() { _ = storage.Close() })
	fixture := mailNewsRegisterRecordFixture(t, storage)
	if err := mailNewsRecoverRotation(storage, fixture); err != nil {
		t.Fatal(err)
	}
	source, found := mailNewsReadValue(t, storage, fixture.queue, vault.Key(intent.Source))
	if !found || source != intent.Row {
		t.Fatalf("recovered rotation source = %#v/%t", source, found)
	}
	if _, found := mailNewsReadValue(t, storage, fixture.queue, vault.Key(intent.Target)); found {
		t.Fatal("recovered rotation retained target")
	}
	if _, found := mailNewsReadValue(t, storage, fixture.rotation, vault.Key("rotation")); found {
		t.Fatal("recovered rotation retained intent")
	}
	mailNewsSeedRotation(t, storage, fixture, intent)
	if err := mailNewsApplyRotation(storage, fixture, intent); err != nil {
		t.Fatal(err)
	}
	if err := mailNewsClearRotationIntent(storage, fixture); err != nil {
		t.Fatal(err)
	}
	if _, found := mailNewsReadValue(t, storage, fixture.queue, vault.Key(intent.Source)); found {
		t.Fatal("retried rotation retained source")
	}
	target, found := mailNewsReadValue(t, storage, fixture.queue, vault.Key(intent.Target))
	if !found || target != intent.Row {
		t.Fatalf("retried rotation target = %#v/%t", target, found)
	}
}

func mailNewsFindCursorExpectations(
	t *testing.T,
	engine *engine,
) []mailNewsCursorExpectation {
	t.Helper()
	queues := []string{"incoming", "processed", "outgoing", "published"}
	for _, earlier := range queues {
		earlierShard := engine.route(mailNewsCursorBucket, vault.Key(earlier))
		for _, later := range queues {
			laterShard := engine.route(mailNewsCursorBucket, vault.Key(later))
			if earlierShard >= laterShard {
				continue
			}

			return []mailNewsCursorExpectation{
				{
					queue:    earlier,
					sequence: 101,
					row:      mailNewsQueueKey(earlier, 101),
					shard:    earlierShard,
				},
				{queue: later, sequence: 202, row: mailNewsQueueKey(later, 202), shard: laterShard},
			}
		}
	}
	t.Fatal("no ordered distinct-shard cursor pair found")

	return nil
}

func mailNewsSeedCursorRows(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	expectations []mailNewsCursorExpectation,
) {
	t.Helper()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		for _, expectation := range expectations {
			row := mailNewsWireRow{
				Identity: expectation.queue, Payload: "cursor", Sequence: expectation.sequence,
			}
			if err := fixture.queue.Put(tx, expectation.row, row); err != nil {
				return fmt.Errorf("seed cursor queue row: %w", err)
			}
			if err := fixture.cursors.Put(tx, vault.Key(expectation.queue), 1); err != nil {
				return fmt.Errorf("seed cursor floor: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mailNewsReconcileCursorFloors(
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
) error {
	floors, err := mailNewsQueueFloors(storage)
	if err != nil {
		return err
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		for queue, sequence := range floors {
			if err := fixture.cursors.Put(tx, vault.Key(queue), sequence); err != nil {
				return fmt.Errorf("store cursor floor: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("reconcile cursor floors: %w", err)
	}

	return nil
}

func mailNewsQueueFloors(storage *vault.Vault) (map[string]uint64, error) {
	floors := make(map[string]uint64)
	var after vault.Key
	for {
		var page vault.BucketKeyPage
		if err := storage.View(context.Background(), func(tx *vault.Txn) error {
			var err error
			page, err = tx.ReadBucketKeyPage(mailNewsQueueBucket, after, 128)
			if err != nil {
				return fmt.Errorf("read queued cursor page: %w", err)
			}

			return nil
		}); err != nil {
			return nil, fmt.Errorf("scan queued cursor page: %w", err)
		}
		for _, key := range page.Keys {
			queue, sequence, err := mailNewsParseQueueKey(key)
			if err != nil {
				return nil, err
			}
			floors[queue] = max(floors[queue], sequence)
		}
		if len(page.Keys) == 0 || !page.More {
			return floors, nil
		}
		after = page.Keys[len(page.Keys)-1]
	}
}

func mailNewsParseQueueKey(key vault.Key) (string, uint64, error) {
	separator := slices.Index(key, byte('/'))
	if separator <= 0 || len(key) != separator+1+8 {
		return "", 0, fmt.Errorf("invalid mail/news queue key %q", key)
	}

	return string(key[:separator]), binary.BigEndian.Uint64(key[separator+1:]), nil
}

func mailNewsAssertCursorFloorsRecovered(
	t *testing.T,
	directory string,
	expectations []mailNewsCursorExpectation,
) {
	t.Helper()
	engine, storage := mailNewsOpenStorage(t, directory)
	t.Cleanup(func() { _ = storage.Close() })
	fixture := mailNewsRegisterRecordFixture(t, storage)
	if err := mailNewsReconcileCursorFloors(storage, fixture); err != nil {
		t.Fatal(err)
	}
	for _, expectation := range expectations {
		mailNewsAssertCursorValue(t, storage, fixture, expectation, expectation.sequence)
		row, found := mailNewsReadValue(t, storage, fixture.queue, expectation.row)
		if !found || row.Sequence != expectation.sequence {
			t.Fatalf("recovered %s queue row = %#v/%t", expectation.queue, row, found)
		}
	}
	if got := mailNewsBucketRows(t, engine, mailNewsQueueBucket); got != len(expectations) {
		t.Fatalf("recovered cursor queue rows = %d, want %d", got, len(expectations))
	}
}

func mailNewsAssertCursorValue(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsRecordFixture,
	expectation mailNewsCursorExpectation,
	want uint64,
) {
	t.Helper()
	got, found := mailNewsReadValue(t, storage, fixture.cursors, vault.Key(expectation.queue))
	if !found || got != want {
		t.Fatalf("%s cursor floor = %d/%t, want %d", expectation.queue, got, found, want)
	}
}

func mailNewsQueueKey(queue string, sequence uint64) vault.Key {
	key := append(vault.Key(queue+"/"), make([]byte, 8)...)
	binary.BigEndian.PutUint64(key[len(key)-8:], sequence)

	return key
}

func mailNewsReadValue[V any](
	t *testing.T,
	storage *vault.Vault,
	space *vault.Keyspace[V],
	key vault.Key,
) (V, bool) {
	t.Helper()
	var value V
	var found bool
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		value, found, err = space.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read mail/news fixture value: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return value, found
}

func mailNewsBucketRows(t *testing.T, engine *engine, bucket vault.Name) int {
	t.Helper()
	rows := 0
	for _, database := range engine.shards {
		if err := database.View(func(tx *bolt.Tx) error {
			stored := tx.Bucket([]byte(bucket))
			if stored == nil {
				return nil
			}

			return stored.ForEach(func(_, _ []byte) error {
				rows++

				return nil
			})
		}); err != nil {
			t.Fatal(err)
		}
	}

	return rows
}
