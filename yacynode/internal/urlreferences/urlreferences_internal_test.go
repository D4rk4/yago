package urlreferences

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

func openScriptedReferences(t *testing.T) (*vault.Vault, *urlReferences, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	references, err := openURLReferences(storage)
	if err != nil {
		t.Fatalf("openURLReferences: %v", err)
	}
	return storage, references, engine
}

func corruptLength(t *testing.T, engine *scriptedEngine) {
	t.Helper()
	for name, bucket := range engine.buckets {
		if name != wordsByURLBucket && name != referencedURLBucket {
			bucket[string(referencedURLBucket)] = []byte("bad")
			return
		}
	}
	t.Fatal("length bucket not found")
}

func TestOpenURLReferencesReturnsRegisterErrors(t *testing.T) {
	if _, err := openURLReferences(nil); err == nil {
		t.Fatal("expected first register error")
	}

	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Register(storage, referencedURLBucket, presenceCodec{}); err != nil {
		t.Fatal(err)
	}
	if _, err := openURLReferences(storage); err == nil {
		t.Fatal("expected second register error")
	}
}

func TestPostingStoredReturnsWriteErrors(t *testing.T) {
	storage, references, engine := openScriptedReferences(t)
	word := yacymodel.WordHash("w1")
	url := yacymodel.WordHash("u1")

	engine.putErrors[wordsByURLBucket] = errors.New("words put failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingStored(tx, word, url)
	}); err == nil {
		t.Fatal("expected words put error")
	}

	engine.putErrors[wordsByURLBucket] = nil
	engine.putErrors[referencedURLBucket] = errors.New("referenced put failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingStored(tx, word, url)
	}); err == nil {
		t.Fatal("expected referenced put error")
	}
}

func TestPostingPurgedReturnsDeleteAndReferenceErrors(t *testing.T) {
	storage, references, engine := openScriptedReferences(t)
	word := yacymodel.WordHash("w1")
	url := yacymodel.WordHash("u1")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingStored(tx, word, url)
	}); err != nil {
		t.Fatal(err)
	}

	engine.deleteErrors[wordsByURLBucket] = errors.New("words delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingPurged(tx, word, url)
	}); err == nil {
		t.Fatal("expected words delete error")
	}

	engine.deleteErrors[wordsByURLBucket] = nil
	engine.scanErrors[wordsByURLBucket] = errors.New("scan failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingPurged(tx, word, url)
	}); err == nil {
		t.Fatal("expected words scan error")
	}

	engine.scanErrors[wordsByURLBucket] = nil
	engine.deleteErrors[referencedURLBucket] = errors.New("referenced delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingPurged(tx, word, url)
	}); err == nil {
		t.Fatal("expected referenced delete error")
	}
}

func TestWordsReferencingReturnsBadKeyAndScanErrors(t *testing.T) {
	storage, references, engine := openScriptedReferences(t)
	url := yacymodel.WordHash("u1")
	engine.buckets[wordsByURLBucket][url.String()+"short"] = []byte{}

	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, err := references.WordsReferencing(tx, url)
		return err
	}); err == nil {
		t.Fatal("expected bad word key error")
	}

	engine.buckets[wordsByURLBucket] = map[string][]byte{}
	engine.scanErrors[wordsByURLBucket] = errors.New("scan failed")
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, err := references.WordsReferencing(tx, url)
		return err
	}); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestReferencedURLCountReturnsErrors(t *testing.T) {
	storage, references, engine := openScriptedReferences(t)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return references.PostingStored(tx, yacymodel.WordHash("w1"), yacymodel.WordHash("u1"))
	}); err != nil {
		t.Fatal(err)
	}
	corruptLength(t, engine)
	if _, err := references.ReferencedURLCount(t.Context()); err == nil {
		t.Fatal("expected count length error")
	}

	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := references.ReferencedURLCount(t.Context()); err == nil {
		t.Fatal("expected count view error")
	}
}

func TestWordFromKeyRejectsBadLength(t *testing.T) {
	if _, err := wordFromKey(vault.Key("bad")); err == nil {
		t.Fatal("expected key length error")
	}
}
