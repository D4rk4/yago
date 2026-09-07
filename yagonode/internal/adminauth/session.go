package adminauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	sessionCookieName = "yago_admin_session"
	sessionTokenBytes = 32
	csrfTokenBytes    = 32
)

type session struct {
	Token     string
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func newRandomToken(size int) string {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)

	return base64.RawURLEncoding.EncodeToString(buf)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func sessionCookie(
	name, path, token string,
	secure bool,
	expires time.Time,
) *http.Cookie {
	//nolint:gosec // G124: Secure follows the request TLS state so the admin cookie reaches loopback HTTP; HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{ // nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure follows request TLS; HttpOnly and SameSite=Strict are always set.
		Name:     name,
		Value:    token,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
	}
}

func clearedSessionCookie(name, path string, secure bool) *http.Cookie {
	//nolint:gosec // G124: mirrors sessionCookie so the cleared cookie matches attributes; Secure follows request TLS, HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{ // nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure follows request TLS; HttpOnly and SameSite=Strict are always set.
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}
