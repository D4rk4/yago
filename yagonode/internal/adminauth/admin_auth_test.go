package adminauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfigAppliesDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()
	if cfg.SessionTTL != DefaultSessionTTL ||
		cfg.LoginMaxFailures != DefaultLoginMaxFailures ||
		cfg.LoginWindow != DefaultLoginWindow ||
		cfg.Now == nil {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
}

func TestConfigPreservesExplicitValues(t *testing.T) {
	cfg := Config{
		SessionTTL:       time.Second,
		LoginMaxFailures: 9,
		LoginWindow:      time.Hour,
		Now:              time.Now,
	}.withDefaults()
	if cfg.SessionTTL != time.Second || cfg.LoginMaxFailures != 9 || cfg.LoginWindow != time.Hour {
		t.Fatalf("explicit values overwritten: %#v", cfg)
	}
}

func TestNewRegistersStores(t *testing.T) {
	if _, err := New(testVault(t), Config{}); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNewSurfacesCredentialRegistrationError(t *testing.T) {
	storage := testVault(t)
	if _, err := newCredentialStore(storage); err != nil {
		t.Fatalf("pre-register credentials: %v", err)
	}
	if _, err := New(storage, Config{}); err == nil {
		t.Fatal("New should fail when the credential store cannot register")
	}
}

func TestNewSurfacesSessionRegistrationError(t *testing.T) {
	storage := testVault(t)
	if _, err := newSessionStore(storage, time.Hour, time.Now); err != nil {
		t.Fatalf("pre-register sessions: %v", err)
	}
	if _, err := New(storage, Config{}); err == nil {
		t.Fatal("New should fail when the session store cannot register")
	}
}

func TestBootstrapFromEnv(t *testing.T) {
	ctx := context.Background()
	service := testService(t)

	if err := service.BootstrapFromEnv(ctx, "", ""); err != nil {
		t.Fatalf("empty bootstrap: %v", err)
	}
	if present, _ := service.creds.exists(ctx); present {
		t.Fatal("empty bootstrap must not create an admin")
	}

	if err := service.BootstrapFromEnv(ctx, "admin", "pw"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if ok, _ := service.creds.verify(ctx, "admin", "pw"); !ok {
		t.Fatal("bootstrapped admin should verify")
	}
}

func TestBootstrapFromEnvSurfacesError(t *testing.T) {
	engine := newScriptedEngine()
	engine.putErr = errors.New("disk full")
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := service.BootstrapFromEnv(context.Background(), "admin", "pw"); err == nil {
		t.Fatal("bootstrap should surface the store error")
	}
}
