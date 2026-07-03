package adminauth

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	DefaultSessionTTL       = 12 * time.Hour
	DefaultLoginMaxFailures = 5
	DefaultLoginWindow      = 15 * time.Minute
)

type Config struct {
	SessionTTL       time.Duration
	LoginMaxFailures int
	LoginWindow      time.Duration
	Now              func() time.Time
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
	if c.Now == nil {
		c.Now = time.Now
	}

	return c
}

type Service struct {
	creds    *credentialStore
	sessions *sessionStore
	limiter  *loginRateLimiter
	now      func() time.Time
}

func New(storage *vault.Vault, cfg Config) (*Service, error) {
	cfg = cfg.withDefaults()
	creds, err := newCredentialStore(storage)
	if err != nil {
		return nil, err
	}
	sessions, err := newSessionStore(storage, cfg.SessionTTL, cfg.Now)
	if err != nil {
		return nil, err
	}

	return &Service{
		creds:    creds,
		sessions: sessions,
		limiter:  newLoginRateLimiter(cfg.LoginMaxFailures, cfg.LoginWindow, cfg.Now),
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
