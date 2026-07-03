package adminauth

import (
	"net/http"
	"testing"
	"time"
)

type countingObserver struct {
	loginSuccess   int
	loginFailure   int
	loginThrottled int
	keyRejected    int
	keyThrottled   int
	keyForbidden   int
}

func (c *countingObserver) LoginSucceeded()  { c.loginSuccess++ }
func (c *countingObserver) LoginFailed()     { c.loginFailure++ }
func (c *countingObserver) LoginThrottled()  { c.loginThrottled++ }
func (c *countingObserver) APIKeyRejected()  { c.keyRejected++ }
func (c *countingObserver) APIKeyThrottled() { c.keyThrottled++ }
func (c *countingObserver) APIKeyForbidden() { c.keyForbidden++ }

func observerService(t *testing.T, obs AuthObserver) *Service {
	t.Helper()
	service, err := New(testVault(t), Config{
		Observer:           obs,
		LoginMaxFailures:   1,
		LoginWindow:        time.Minute,
		APIKeyMaxPerWindow: 1,
		APIKeyWindow:       time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service
}

func TestObserverRecordsLoginSuccess(t *testing.T) {
	obs := &countingObserver{}
	handler := mountAuth(t, observerService(t, obs))
	body := `{"username":"admin","password":"pw"}`
	if rec := doRequest(handler, http.MethodPost, PathSetup, body); rec.Code != http.StatusCreated {
		t.Fatalf("setup = %d", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, PathLogin, body); rec.Code != http.StatusOK {
		t.Fatalf("login = %d", rec.Code)
	}
	if obs.loginSuccess != 1 {
		t.Fatalf("loginSuccess = %d, want 1", obs.loginSuccess)
	}
}

func TestObserverRecordsLoginFailureAndThrottle(t *testing.T) {
	obs := &countingObserver{}
	handler := mountAuth(t, observerService(t, obs))
	if rec := doRequest(
		handler,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("setup = %d", rec.Code)
	}
	wrong := `{"username":"admin","password":"nope"}`
	if rec := doRequest(
		handler,
		http.MethodPost,
		PathLogin,
		wrong,
	); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first wrong login = %d, want 401", rec.Code)
	}
	if rec := doRequest(
		handler,
		http.MethodPost,
		PathLogin,
		wrong,
	); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second wrong login = %d, want 429", rec.Code)
	}
	if obs.loginFailure != 1 || obs.loginThrottled != 1 {
		t.Fatalf("failure=%d throttled=%d, want 1/1", obs.loginFailure, obs.loginThrottled)
	}
}

func TestObserverRecordsAPIKeyRejectedAndForbidden(t *testing.T) {
	obs := &countingObserver{}
	service := observerService(t, obs)
	surface := apiKeyGuarded(t, service)

	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		"not-a-key",
	); rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed bearer = %d, want 401", rec.Code)
	}
	key := createKey(t, service, ScopeSearchRead)
	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		key.Key,
	); rec.Code != http.StatusForbidden {
		t.Fatalf("insufficient scope = %d, want 403", rec.Code)
	}
	if obs.keyRejected != 1 || obs.keyForbidden != 1 {
		t.Fatalf("rejected=%d forbidden=%d, want 1/1", obs.keyRejected, obs.keyForbidden)
	}
}

func TestObserverRecordsAPIKeyThrottled(t *testing.T) {
	obs := &countingObserver{}
	service := observerService(t, obs)
	surface := apiKeyGuarded(t, service)
	key := createKey(t, service, ScopeAdminRead)

	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		key.Key,
	); rec.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", rec.Code)
	}
	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		key.Key,
	); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", rec.Code)
	}
	if obs.keyThrottled != 1 {
		t.Fatalf("keyThrottled = %d, want 1", obs.keyThrottled)
	}
}
