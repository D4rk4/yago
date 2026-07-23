package crawlformats

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type formatReadEngine struct {
	raw []byte
}

func (e *formatReadEngine) Provision(vault.Name) error { return nil }

func (e *formatReadEngine) Update(
	_ context.Context,
	apply func(vault.EngineTxn) error,
) error {
	return apply(formatReadTransaction{engine: e, writable: true})
}

func (e *formatReadEngine) View(
	_ context.Context,
	apply func(vault.EngineTxn) error,
) error {
	return apply(formatReadTransaction{engine: e})
}

func (e *formatReadEngine) Close() error { return nil }

func (e *formatReadEngine) QuotaBytes() int64 { return 0 }

func (e *formatReadEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

type formatReadTransaction struct {
	engine   *formatReadEngine
	writable bool
}

func (t formatReadTransaction) Writable() bool { return t.writable }

func (t formatReadTransaction) Bucket(vault.Name) vault.EngineBucket {
	return formatReadBucket{engine: t.engine}
}

type formatReadBucket struct {
	engine *formatReadEngine
}

func (b formatReadBucket) Get(key vault.Key) []byte {
	if string(key) != string(togglesKey) {
		return nil
	}

	return b.engine.raw
}

func (b formatReadBucket) Put(_ vault.Key, value []byte) error {
	b.engine.raw = append([]byte(nil), value...)

	return nil
}

func (b formatReadBucket) Delete(vault.Key) error {
	b.engine.raw = nil

	return nil
}

func (formatReadBucket) Scan(
	vault.Key,
	func(vault.Key, []byte) (bool, error),
) error {
	return nil
}

func TestCurrentSurfacesReadFailures(t *testing.T) {
	t.Parallel()

	v, err := vault.New(&formatReadEngine{raw: []byte("{invalid")})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("corrupt persisted toggles must fail at open")
	}

	clean, err := vault.New(&formatReadEngine{})
	if err != nil {
		t.Fatalf("clean vault.New: %v", err)
	}
	store, err := Open(clean)
	if err != nil {
		t.Fatalf("clean Open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Current(ctx); err == nil {
		t.Fatal("cancelled format read must fail")
	}

	persisted := yagocrawlcontract.FormatToggles{Text: true, Archives: true}
	encoded, err := (togglesCodec{}).Encode(persisted)
	if err != nil {
		t.Fatalf("encode persisted toggles: %v", err)
	}
	loadedVault, err := vault.New(&formatReadEngine{raw: encoded})
	if err != nil {
		t.Fatalf("persisted vault.New: %v", err)
	}
	loaded, err := Open(loadedVault)
	if err != nil {
		t.Fatalf("open persisted toggles: %v", err)
	}
	current, err := loaded.Current(t.Context())
	if err != nil {
		t.Fatalf("current persisted toggles: %v", err)
	}
	if current != persisted {
		t.Fatalf("persisted toggles = %+v, want %+v", current, persisted)
	}
}
