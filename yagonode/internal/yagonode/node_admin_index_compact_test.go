package yagonode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type compactionStoreFixture struct {
	headroom uint64
	result   vault.CompactResult
	err      error
	calls    int
}

func (store *compactionStoreFixture) CompactionHeadroom(context.Context) (uint64, error) {
	return store.headroom, nil
}

func (store *compactionStoreFixture) Compact(
	context.Context,
) (vault.CompactResult, error) {
	store.calls++

	return store.result, store.err
}

func TestCompactorSourceMapsResult(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	source := newCompactorSource(v)
	if source == nil {
		t.Fatal("newCompactorSource returned nil for a real vault")
	}
	result, err := source.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// The in-memory engine has no files to compact, so the adapter maps a zero
	// result with a formatted byte figure.
	if result.ShardsCompacted != 0 || result.BytesReclaimed != "0 B" {
		t.Fatalf("result = %+v, want {0, \"0 B\"}", result)
	}
}

func TestCompactorSourceReportsInsufficientTemporaryCopyHeadroom(t *testing.T) {
	store := &compactionStoreFixture{headroom: 7 << 30}
	admission := &nodeGrowthAdmission{err: yagocrawlcontract.ErrStorageHeadroom}
	source := compactorSource{store: store, admission: admission}
	_, err := source.Compact(t.Context())
	var operatorError adminui.CompactionOperatorError
	if !errors.As(err, &operatorError) ||
		!strings.Contains(operatorError.CompactionOperatorMessage(), "7.0 GiB") ||
		store.calls != 0 || admission.requiredHeadroom != 7<<30 {
		t.Fatalf(
			"headroom error=%v calls=%d required=%d",
			err,
			store.calls,
			admission.requiredHeadroom,
		)
	}
	if err == nil || err.Error() != operatorError.CompactionOperatorMessage() {
		t.Fatalf("operator error text = %v", err)
	}
}

func TestCompactorSourceRunsPositiveCompactionAndReportsFailure(t *testing.T) {
	store := &compactionStoreFixture{
		headroom: 1,
		result:   vault.CompactResult{ShardsCompacted: 2, BytesReclaimed: 2048},
	}
	result, err := (compactorSource{store: store}).Compact(t.Context())
	if err != nil || store.calls != 1 || result.ShardsCompacted != 2 ||
		result.BytesReclaimed != "2.0 KiB" {
		t.Fatalf("positive compaction result=%+v calls=%d error=%v", result, store.calls, err)
	}
	store.err = errors.New("compaction failed")
	_, err = (compactorSource{store: store}).Compact(t.Context())
	if err == nil || !strings.Contains(err.Error(), "compaction failed") {
		t.Fatalf("failed compaction error = %v", err)
	}
}

func TestNewCompactorSourceNilVault(t *testing.T) {
	if newCompactorSource(nil) != nil {
		t.Fatal("newCompactorSource(nil) must return a nil Compactor")
	}
}
