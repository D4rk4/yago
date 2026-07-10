package vault_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// readDeferCapableEngine embeds doubleEngine to inherit the full Engine surface
// and adds the optional read-defer capability with injectable results, so every
// branch of the Vault read-defer forwarders can be driven.
type readDeferCapableEngine struct {
	*doubleEngine
	deferred   time.Duration
	setCalls   int
	lastBudget time.Duration
}

func (e *readDeferCapableEngine) SetReadDeferBudget(budget time.Duration) {
	e.setCalls++
	e.lastBudget = budget
}

func (e *readDeferCapableEngine) ReadDeferred() time.Duration { return e.deferred }

func openReadDeferCapable(t *testing.T, engine *readDeferCapableEngine) *vault.Vault {
	t.Helper()
	engine.doubleEngine = &doubleEngine{buckets: map[vault.Name]map[string][]byte{}}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new read-defer-capable vault: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close read-defer-capable vault: %v", err)
		}
	})

	return v
}

func TestVaultSetReadDeferBudgetCoversAllBranches(t *testing.T) {
	var nilVault *vault.Vault
	nilVault.SetReadDeferBudget(time.Second)

	openClosedDouble(t).SetReadDeferBudget(time.Second)
	openLiveDouble(t).SetReadDeferBudget(time.Second)

	engine := &readDeferCapableEngine{}
	openReadDeferCapable(t, engine).SetReadDeferBudget(250 * time.Millisecond)
	if engine.setCalls != 1 || engine.lastBudget != 250*time.Millisecond {
		t.Fatalf("SetReadDeferBudget forwarded calls=%d budget=%v, want 1/250ms",
			engine.setCalls, engine.lastBudget)
	}
}

func TestVaultReadDeferredCoversAllBranches(t *testing.T) {
	var nilVault *vault.Vault
	if nilVault.ReadDeferred() != 0 {
		t.Fatal("nil vault reported read deferral")
	}
	if openClosedDouble(t).ReadDeferred() != 0 {
		t.Fatal("closed vault reported read deferral")
	}
	if openLiveDouble(t).ReadDeferred() != 0 {
		t.Fatal("engine without the capability reported read deferral")
	}
	capable := openReadDeferCapable(t, &readDeferCapableEngine{deferred: 7 * time.Second})
	if got := capable.ReadDeferred(); got != 7*time.Second {
		t.Fatalf("capable engine ReadDeferred = %v, want 7s", got)
	}
}
