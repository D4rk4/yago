package yagonode

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// ctrlEngine is a controllable in-memory vault engine that can fail scans (all,
// or after a budget) and corrupt a bucket's stored bytes, so defensive error
// branches guarded by store operations become reachable from a test.
type ctrlEngine struct {
	mu             sync.Mutex
	buckets        map[vault.Name]map[string][]byte
	failScan       bool
	scanBudget     int // 0 disables the budget; otherwise fail once this many scans ran
	scans          int
	failProvision  map[vault.Name]bool
	failScanBucket map[vault.Name]bool
	// failUpdate rejects every writable transaction while leaving read views
	// intact, so a write that follows a successful read can be failed on its own.
	failUpdate bool
}

func newCtrlEngine() *ctrlEngine {
	return &ctrlEngine{
		buckets:        map[vault.Name]map[string][]byte{},
		failProvision:  map[vault.Name]bool{},
		failScanBucket: map[vault.Name]bool{},
	}
}

func (e *ctrlEngine) bucket(name vault.Name) map[string][]byte {
	b, ok := e.buckets[name]
	if !ok {
		b = map[string][]byte{}
		e.buckets[name] = b
	}

	return b
}

func (e *ctrlEngine) corrupt(name vault.Name) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key := range e.bucket(name) {
		e.bucket(name)[key] = []byte("corrupt-not-decodable")
	}
}

func (e *ctrlEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failProvision[name] {
		return errors.New("provision failed for " + string(name))
	}
	e.bucket(name)

	return nil
}

func (e *ctrlEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failUpdate {
		return errors.New("update failed")
	}

	return fn(&ctrlTxn{engine: e, writable: true})
}

func (e *ctrlEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return fn(&ctrlTxn{engine: e, writable: false})
}

func (e *ctrlEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (e *ctrlEngine) QuotaBytes() int64 { return 0 }

func (e *ctrlEngine) Close() error { return nil }

type ctrlTxn struct {
	engine   *ctrlEngine
	writable bool
}

func (t *ctrlTxn) Bucket(name vault.Name) vault.EngineBucket {
	return &ctrlBucket{engine: t.engine, name: name, data: t.engine.bucket(name)}
}

func (t *ctrlTxn) Writable() bool { return t.writable }

type ctrlBucket struct {
	engine *ctrlEngine
	name   vault.Name
	data   map[string][]byte
}

func (b *ctrlBucket) Get(key vault.Key) []byte { return b.data[string(key)] }

func (b *ctrlBucket) Put(key vault.Key, value []byte) error {
	stored := make([]byte, len(value))
	copy(stored, value)
	b.data[string(key)] = stored

	return nil
}

func (b *ctrlBucket) Delete(key vault.Key) error {
	delete(b.data, string(key))

	return nil
}

func (b *ctrlBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	b.engine.scans++
	if b.engine.failScan || b.engine.failScanBucket[b.name] {
		return errors.New("scan failed")
	}
	if b.engine.scanBudget > 0 && b.engine.scans > b.engine.scanBudget {
		return errors.New("scan budget exhausted")
	}
	for key, value := range b.data {
		if !bytes.HasPrefix([]byte(key), prefix) {
			continue
		}
		if cont, err := fn(vault.Key(key), value); err != nil || !cont {
			return err
		}
	}

	return nil
}

func ctrlVault(t *testing.T, engine *ctrlEngine) *vault.Vault {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	return v
}

func TestRunNodeProvisionObservabilityError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failScanBucket["events"] = true
	v := ctrlVault(t, engine)
	config := testConfig(t)

	if err := bootNode(context.Background(), config, v); err == nil {
		t.Fatal("bootNode should surface the observability provisioning error")
	}
}

func TestRunNodeLoadRuntimeSettingsError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failScanBucket["runtime_settings"] = true
	v := ctrlVault(t, engine)
	config := testConfig(t)

	if err := bootNode(context.Background(), config, v); err == nil {
		t.Fatal("bootNode should surface the runtime-settings load error")
	}
}

func TestOpenPeerStoresPeerBlockOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["peerblock"] = true
	v := ctrlVault(t, engine)

	_, _, _, _, err := openPeerStores(
		t.Context(), v, yagomodel.Hash("LLLLLLLLLLLL"), nil,
	)
	if err == nil || !strings.Contains(err.Error(), "open peer blocklist") {
		t.Fatalf("openPeerStores error = %v, want peer blocklist open error", err)
	}
}

func TestRunNodeValidateBindsError(t *testing.T) {
	engine := newCtrlEngine()
	v := ctrlVault(t, engine)
	config := testConfig(t)
	config.PeerAddr = "invalid-bind-without-port"

	if err := bootNode(context.Background(), config, v); err == nil {
		t.Fatal("bootNode should reject an invalid listen address")
	}
}

func TestBuildCrawlRuntimeFrontierOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["recrawl_records"] = true
	v := ctrlVault(t, engine)

	_, err := buildRuntimeCrawl(
		context.Background(),
		crawlConfig{ListenAddr: "127.0.0.1:0"},
		nodeIdentity(testConfig(t)),
		nodeStorage{},
		v,
	)
	if err == nil {
		t.Fatal("buildCrawlRuntime should surface the recrawl frontier open error")
	}
}

func TestAssembleNodeSurfacesRankingOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["rankingprofile"] = true
	v := ctrlVault(t, engine)

	_, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx:    context.Background(),
		config: testConfig(t),
		vault:  v,
	})
	if err == nil {
		t.Fatal("assembleNodeSurfaces should surface the ranking profile open error")
	}
}

func TestAssembleNodeSurfacesDenylistOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["urldenylist"] = true
	v := ctrlVault(t, engine)

	_, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx:    context.Background(),
		config: testConfig(t),
		vault:  v,
	})
	if err == nil {
		t.Fatal("assembleNodeSurfaces should surface the url denylist open error")
	}
}

func TestLoadRuntimeSettingsAllError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failScan = true
	v := ctrlVault(t, engine)

	if _, _, _, err := loadRuntimeSettings(
		context.Background(), v, testConfig(t), nil,
	); err == nil {
		t.Fatal("loadRuntimeSettings should surface the All() scan error")
	}
}

func TestLoadRuntimeSettingsInitializationWriteError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failUpdate = true
	v := ctrlVault(t, engine)
	if _, _, _, err := loadRuntimeSettings(
		context.Background(), v, testConfig(t), nil,
	); err == nil {
		t.Fatal("loadRuntimeSettings should surface initialization write failure")
	}
}

func TestAttachDurableEventsRecentError(t *testing.T) {
	engine := newCtrlEngine()
	engine.scanBudget = 1 // resume() scan succeeds; Recent()'s scan fails
	v := ctrlVault(t, engine)

	if _, err := attachDurableEvents(
		context.Background(), v, events.NewRecorder(4),
	); err == nil {
		t.Fatal("attachDurableEvents should surface the Recent() scan error")
	}
}

func TestSweepRedispatchProfileLookupError(t *testing.T) {
	engine := newCtrlEngine()
	v := ctrlVault(t, engine)
	frontier, err := recrawlfrontier.Open(v)
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	seedDueURL(t, frontier, "https://a.example/")
	engine.corrupt("recrawl_profiles")

	publisher := &capturingPublisher{}
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		mint:      func() []byte { return []byte("p") },
		now:       func() time.Time { return sweepBase.Add(2 * time.Hour) },
		batch:     16,
	}
	sweeper.sweepOnce(context.Background())

	if orders := publisher.snapshot(); len(orders) != 0 {
		t.Fatalf("a corrupt profile must publish nothing, got %d", len(orders))
	}
}

func TestSecurityChangePasswordVerifyError(t *testing.T) {
	engine := newCtrlEngine()
	v := ctrlVault(t, engine)
	config := nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}}
	service, err := provisionAdminAuth(context.Background(), config, v, nil)
	if err != nil {
		t.Fatalf("provisionAdminAuth: %v", err)
	}
	source := newSecuritySource(service)

	var gotErr error
	const capturePath = "/api/admin/v1/console/change-password-verify"
	mux := http.NewServeMux()
	mux.HandleFunc(capturePath, func(_ http.ResponseWriter, r *http.Request) {
		_, gotErr = source.ChangePassword(r.Context(), adminui.PasswordChange{
			Current: "pw", New: "np", Confirm: "np",
		})
	})
	handler := guardAdminSurface(service, mux)

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/admin/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"pw"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", loginRec.Code)
	}

	// Corrupt the stored admin credential after login so the session stays valid
	// but the in-handler password verify fails with a non-mismatch error.
	engine.corrupt("adminauth_credentials")

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, capturePath, nil,
	)
	for _, cookie := range loginRec.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe = %d, want 200", rec.Code)
	}
	if gotErr == nil {
		t.Fatal("a corrupt credential must surface a change-password error")
	}
}
