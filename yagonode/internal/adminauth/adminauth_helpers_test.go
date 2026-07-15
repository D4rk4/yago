package adminauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func fixedNow(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time { return c.now }

func testVault(t *testing.T) *vault.Vault {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	return storage
}

func testService(t *testing.T) *Service {
	t.Helper()
	service, err := New(testVault(t), Config{
		SessionTTL:       time.Hour,
		LoginMaxFailures: 3,
		LoginWindow:      time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service
}

type scriptedEngine struct {
	buckets   map[vault.Name]map[string][]byte
	putErr    error
	deleteErr error
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func scriptedVault(t *testing.T, engine *scriptedEngine) *vault.Vault {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	return storage
}

func (e *scriptedEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *scriptedEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(scriptedTxn{engine: e, writable: true})
}

func (e *scriptedEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(scriptedTxn{engine: e})
}

func (e *scriptedEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (e *scriptedEngine) QuotaBytes() int64 { return 0 }

func (e *scriptedEngine) Close() error { return nil }

type scriptedTxn struct {
	engine   *scriptedEngine
	writable bool
}

func (t scriptedTxn) Writable() bool { return t.writable }

func (t scriptedTxn) Bucket(name vault.Name) vault.EngineBucket {
	return scriptedBucket{engine: t.engine, data: t.engine.buckets[name]}
}

type scriptedBucket struct {
	engine *scriptedEngine
	data   map[string][]byte
}

func (b scriptedBucket) Get(key vault.Key) []byte { return b.data[string(key)] }

func (b scriptedBucket) Put(key vault.Key, value []byte) error {
	if b.engine.putErr != nil {
		return b.engine.putErr
	}
	b.data[string(key)] = value

	return nil
}

func (b scriptedBucket) Delete(key vault.Key) error {
	if b.engine.deleteErr != nil {
		return b.engine.deleteErr
	}
	delete(b.data, string(key))

	return nil
}

func (b scriptedBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	ordered := make([]string, 0, len(b.data))
	for key := range b.data {
		if strings.HasPrefix(key, string(prefix)) {
			ordered = append(ordered, key)
		}
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		keep, err := fn(vault.Key(key), b.data[key])
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func injectAdmin(t *testing.T, engine *scriptedEngine, username, password string) {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	data, err := json.Marshal(adminRecord{Username: username, PasswordHash: hash})
	if err != nil {
		t.Fatalf("marshal admin record: %v", err)
	}
	engine.buckets[adminCredentialsBucket][string(adminKey)] = data
}

func mountAuth(t *testing.T, service *Service) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, service)

	return mux
}

func scriptedService(t *testing.T) (*Service, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service, engine
}

func createKey(t *testing.T, service *Service, scopes ...Scope) createdAPIKey {
	t.Helper()
	created, err := service.apiKeys.create(context.Background(), "ci key", scopes)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	return created
}

func doBearerRequest(
	handler http.Handler,
	method, path, token string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(""))
	req.Header.Set(authzHeader, bearerScheme+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func doRequest(
	handler http.Handler,
	method, path, body string,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(
		context.Background(),
		method,
		path,
		strings.NewReader(body),
	)
	if method == http.MethodPost && (path == PathLogin || path == PathSetup) {
		req.Header.Set("Content-Type", authJSONMediaType)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func doRequestWithCSRF(
	handler http.Handler,
	method, path, csrf string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(""))
	req.AddCookie(cookie)
	req.Header.Set(csrfHeader, csrf)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func decodeBody(rec *httptest.ResponseRecorder, target any) error {
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}

	return nil
}

func cookieNamed(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}

	return nil
}
