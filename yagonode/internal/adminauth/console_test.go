package adminauth

import (
	"context"
	"errors"
	"testing"
)

func TestServiceAPIKeyLifecycle(t *testing.T) {
	service := testService(t)
	ctx := context.Background()

	created, err := service.CreateAPIKey(ctx, "ci", []string{"search:read", "crawl:write"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Secret == "" || created.ID == "" {
		t.Fatal("created key is missing its secret or id")
	}
	if _, _, ok := parseAPIKey(created.Secret); !ok {
		t.Fatal("created secret does not parse")
	}

	keys, err := service.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != created.ID || keys[0].Label != "ci" {
		t.Fatalf("list = %#v", keys)
	}

	existed, err := service.RevokeAPIKey(ctx, created.ID)
	if err != nil || !existed {
		t.Fatalf("revoke: existed=%v err=%v", existed, err)
	}
	after, err := service.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("key survived revoke: %#v", after)
	}
}

func TestServiceCreateAPIKeyRejectsUnknownScope(t *testing.T) {
	service := testService(t)
	if _, err := service.CreateAPIKey(context.Background(), "x", []string{"bogus"}); err == nil {
		t.Fatal("expected an error for an unknown scope")
	}
}

func TestServiceRevokeAPIKeyMissing(t *testing.T) {
	service := testService(t)
	existed, err := service.RevokeAPIKey(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("revoke missing: %v", err)
	}
	if existed {
		t.Fatal("revoke reported a non-existent key as existing")
	}
}

func TestServiceChangePassword(t *testing.T) {
	service := testService(t)
	ctx := context.Background()
	if err := service.BootstrapFromEnv(ctx, "admin", "old-password-123"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	err := service.ChangePassword(ctx, "admin", "wrong-password", "new-password-456")
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("expected ErrPasswordMismatch, got %v", err)
	}
	if err := service.ChangePassword(
		ctx,
		"admin",
		"old-password-123",
		"new-password-456",
	); err != nil {
		t.Fatalf("change with the correct current password: %v", err)
	}
	if err := service.ChangePassword(
		ctx,
		"admin",
		"new-password-456",
		"third-password-789",
	); err != nil {
		t.Fatalf("the new password did not take effect: %v", err)
	}
}

func TestKnownScopesAreAllValid(t *testing.T) {
	scopes := KnownScopes()
	if len(scopes) == 0 {
		t.Fatal("KnownScopes returned nothing")
	}
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, string(scope))
	}
	if _, err := parseScopes(names); err != nil {
		t.Fatalf("KnownScopes are not all valid: %v", err)
	}
}
