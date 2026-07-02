package urlmetastaleness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type scriptedEngine struct {
	buckets      map[vault.Name]map[string][]byte
	putErrors    map[vault.Name]error
	deleteErrors map[vault.Name]error
	scanErrors   map[vault.Name]error
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:      map[vault.Name]map[string][]byte{},
		putErrors:    map[vault.Name]error{},
		deleteErrors: map[vault.Name]error{},
		scanErrors:   map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedTxn{engine: e, writable: true})
}

func (e *scriptedEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedTxn{engine: e})
}

func (e *scriptedEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}
	return nil
}

func (e *scriptedEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *scriptedEngine) QuotaBytes() int64                        { return 0 }
func (e *scriptedEngine) Close() error                             { return nil }

type scriptedTxn struct {
	engine   *scriptedEngine
	writable bool
}

func (t scriptedTxn) Bucket(name vault.Name) vault.EngineBucket {
	return scriptedBucket{engine: t.engine, name: name}
}

func (t scriptedTxn) Writable() bool { return t.writable }

type scriptedBucket struct {
	engine *scriptedEngine
	name   vault.Name
}

func (b scriptedBucket) Get(key vault.Key) []byte {
	raw, ok := b.engine.buckets[b.name][string(key)]
	if !ok {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

func (b scriptedBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)
	return nil
}

func (b scriptedBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}
	delete(b.engine.buckets[b.name], string(key))
	return nil
}

func (b scriptedBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.engine.buckets[b.name][key]...))
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}
	return nil
}

func openScriptedOrder(t *testing.T) (*vault.Vault, *stalenessRanking, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	order, err := openStalenessRanking(storage)
	if err != nil {
		t.Fatalf("openStalenessRanking: %v", err)
	}
	return storage, order, engine
}

func TestOpenStalenessRankingReturnsRegisterErrors(t *testing.T) {
	if _, err := openStalenessRanking(nil); err == nil {
		t.Fatal("expected first register error")
	}

	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Register(storage, freshnessBucket, freshnessCodec{}); err != nil {
		t.Fatal(err)
	}
	if _, err := openStalenessRanking(storage); err == nil {
		t.Fatal("expected second register error")
	}
}

func TestHashFromOrderKeyRejectsBadKeys(t *testing.T) {
	if _, err := hashFromOrderKey(vault.Key("bad")); err == nil {
		t.Fatal("expected missing separator error")
	}
	if _, err := hashFromOrderKey(vault.Key("fresh\x00bad")); err == nil {
		t.Fatal("expected bad hash error")
	}
}

func TestStalestURLsReturnsScanAndKeyErrors(t *testing.T) {
	storage, order, engine := openScriptedOrder(t)
	hash := yacymodel.WordHash("u1")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLStored(tx, hash, "20200101")
	}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[orderBucket]["bad"] = []byte{}
	if _, err := order.StalestURLs(t.Context(), 10); err == nil {
		t.Fatal("expected bad key error")
	}

	delete(engine.buckets[orderBucket], "bad")
	engine.scanErrors[orderBucket] = errors.New("scan failed")
	if _, err := order.StalestURLs(t.Context(), 10); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestURLStoredReturnsPutErrors(t *testing.T) {
	storage, order, engine := openScriptedOrder(t)
	hash := yacymodel.WordHash("u1")
	engine.putErrors[orderBucket] = errors.New("order put failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLStored(tx, hash, "20200101")
	}); err == nil {
		t.Fatal("expected order put error")
	}

	engine.putErrors[orderBucket] = nil
	engine.putErrors[freshnessBucket] = errors.New("freshness put failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLStored(tx, hash, "20200101")
	}); err == nil {
		t.Fatal("expected freshness put error")
	}
}

func TestURLPurgedReturnsDeleteErrors(t *testing.T) {
	storage, order, engine := openScriptedOrder(t)
	hash := yacymodel.WordHash("u1")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLStored(tx, hash, "20200101")
	}); err != nil {
		t.Fatal(err)
	}

	engine.deleteErrors[orderBucket] = errors.New("order delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLPurged(tx, hash)
	}); err == nil {
		t.Fatal("expected order delete error")
	}

	engine.deleteErrors[orderBucket] = nil
	engine.deleteErrors[freshnessBucket] = errors.New("freshness delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return order.URLPurged(tx, hash)
	}); err == nil {
		t.Fatal("expected freshness delete error")
	}
}
