package adminauth

import (
	"context"
	"testing"
)

type countingAuthObserver struct {
	noopAuthObserver
	forbidden int
	throttled int
}

func (o *countingAuthObserver) APIKeyForbidden() { o.forbidden++ }
func (o *countingAuthObserver) APIKeyThrottled() { o.throttled++ }

func TestAPIKeyAuthorizerHonoursScope(t *testing.T) {
	observer := &countingAuthObserver{}
	service, err := New(testVault(t), Config{Observer: observer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created, err := service.apiKeys.create(context.Background(), "search", []Scope{ScopeSearchRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()

	if got := authorizer.Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRead,
	); got != APIKeyAuthorized {
		t.Fatalf("read scope = %v, want authorized", got)
	}
	if got := authorizer.Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRaw,
	); got != APIKeyForbidden {
		t.Fatalf("raw scope = %v, want forbidden", got)
	}
	if observer.forbidden != 1 {
		t.Fatalf("forbidden audit = %d, want 1", observer.forbidden)
	}
}

func TestAPIKeyAuthorizerRejectsUnknownAndMalformed(t *testing.T) {
	service, err := New(testVault(t), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created, err := service.apiKeys.create(context.Background(), "search", []Scope{ScopeSearchRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()

	if got := authorizer.Authorize(
		context.Background(),
		"not-a-key",
		ScopeSearchRead,
	); got != APIKeyUnauthenticated {
		t.Fatalf("malformed = %v, want unauthenticated", got)
	}

	empty, err := New(testVault(t), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := empty.APIKeyAuthorizer().Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRead,
	); got != APIKeyUnauthenticated {
		t.Fatalf("unknown key = %v, want unauthenticated", got)
	}
}

func TestAPIKeyAuthorizerThrottles(t *testing.T) {
	observer := &countingAuthObserver{}
	service, err := New(testVault(t), Config{Observer: observer, APIKeyMaxPerWindow: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created, err := service.apiKeys.create(context.Background(), "search", []Scope{ScopeSearchRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()

	if got := authorizer.Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRead,
	); got != APIKeyAuthorized {
		t.Fatalf("first call = %v, want authorized", got)
	}
	if got := authorizer.Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRead,
	); got != APIKeyThrottled {
		t.Fatalf("second call = %v, want throttled", got)
	}
	if observer.throttled != 1 {
		t.Fatalf("throttled audit = %d, want 1", observer.throttled)
	}
}
