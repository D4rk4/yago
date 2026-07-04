package adminauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

const (
	sessionCookieName = "yago_admin_session"
	sessionTokenBytes = 32
	csrfTokenBytes    = 32
)

var randRead = rand.Read

type session struct {
	Token     string
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func newRandomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := randRead(buf); err != nil {
		return "", fmt.Errorf("read random token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func sessionCookie(token string, secure bool, expires time.Time) *http.Cookie {
	//nolint:gosec // G124: Secure follows the request TLS state so the admin cookie reaches loopback HTTP; HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{ // nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure follows request TLS; HttpOnly and SameSite=Strict are always set.
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
	}
}

func clearedSessionCookie(secure bool) *http.Cookie {
	//nolint:gosec // G124: mirrors sessionCookie so the cleared cookie matches attributes; Secure follows request TLS, HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{ // nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure follows request TLS; HttpOnly and SameSite=Strict are always set.
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}
