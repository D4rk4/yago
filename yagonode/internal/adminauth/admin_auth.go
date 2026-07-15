package adminauth

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	DefaultSessionTTL         = 12 * time.Hour
	DefaultLoginMaxFailures   = 5
	DefaultLoginWindow        = 15 * time.Minute
	DefaultAPIKeyMaxPerWindow = 120
	DefaultAPIKeyWindow       = time.Minute
)

type Config struct {
	SessionTTL         time.Duration
	LoginMaxFailures   int
	LoginWindow        time.Duration
	APIKeyMaxPerWindow int
	APIKeyWindow       time.Duration
	Observer           AuthObserver
	Now                func() time.Time
}

func (c Config) withDefaults() Config {
	if c.SessionTTL <= 0 {
		c.SessionTTL = DefaultSessionTTL
	}
	if c.LoginMaxFailures <= 0 {
		c.LoginMaxFailures = DefaultLoginMaxFailures
	}
	if c.LoginWindow <= 0 {
		c.LoginWindow = DefaultLoginWindow
	}
	if c.APIKeyMaxPerWindow <= 0 {
		c.APIKeyMaxPerWindow = DefaultAPIKeyMaxPerWindow
	}
	if c.APIKeyWindow <= 0 {
		c.APIKeyWindow = DefaultAPIKeyWindow
	}
	if c.Observer == nil {
		c.Observer = noopAuthObserver{}
	}
	if c.Now == nil {
		c.Now = time.Now
	}

	return c
}

type Service struct {
	creds               *credentialStore
	sessions            *sessionStore
	apiKeys             *apiKeyStore
	setupFormSigningKey []byte
	limiter             *loginRateLimiter
	keyLimiter          *apiKeyRateLimiter
	observer            AuthObserver
	now                 func() time.Time
	wizardDefaults      SetupDefaults
	wizardApply         SetupApplier
	wizardRestart       func()
}

func New(storage *vault.Vault, cfg Config) (*Service, error) {
	cfg = cfg.withDefaults()
	setupFormSigningKey, err := newSetupFormSigningKey()
	if err != nil {
		return nil, err
	}
	creds, err := newCredentialStore(storage)
	if err != nil {
		return nil, err
	}
	sessions, err := newSessionStore(storage, cfg.SessionTTL, cfg.Now)
	if err != nil {
		return nil, err
	}
	apiKeys, err := newAPIKeyStore(storage, cfg.Now)
	if err != nil {
		return nil, err
	}

	return &Service{
		creds:               creds,
		sessions:            sessions,
		apiKeys:             apiKeys,
		setupFormSigningKey: setupFormSigningKey,
		limiter:             newLoginRateLimiter(cfg.LoginMaxFailures, cfg.LoginWindow, cfg.Now),
		keyLimiter: newAPIKeyRateLimiter(
			cfg.APIKeyMaxPerWindow,
			cfg.APIKeyWindow,
			cfg.Now,
		),
		observer: cfg.Observer,
		now:      cfg.Now,
	}, nil
}

// BootstrapFromEnv provisions the administrator from configuration when both a
// username and password are supplied, so a headless deployment can create the
// admin without the setup endpoint. The supplied credentials are authoritative:
// they are applied on every start.
func (s *Service) BootstrapFromEnv(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return nil
	}
	if err := s.creds.setAdmin(ctx, username, password); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	return nil
}
