package adminauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	setupFormTokenField      = "setup_token"
	setupFormCookieName      = "yago_admin_setup"
	setupFormTokenBytes      = 32
	setupFormSigningKeyBytes = 32
	setupFormTokenLifetime   = 10 * time.Minute
)

var setupFormSigningKeyRead = rand.Read

func newSetupFormSigningKey() ([]byte, error) {
	key := make([]byte, setupFormSigningKeyBytes)
	n, err := setupFormSigningKeyRead(key)
	if err != nil {
		return nil, fmt.Errorf("read setup form signing key: %w", err)
	}
	if n != len(key) {
		return nil, fmt.Errorf("read setup form signing key: got %d bytes, want %d", n, len(key))
	}

	return key, nil
}

func (s *Service) issueSetupFormToken(w http.ResponseWriter, r *http.Request) (string, error) {
	nonce, err := newRandomToken(setupFormTokenBytes)
	if err != nil {
		return "", err
	}
	expires := s.now().Add(setupFormTokenLifetime)
	payload := nonce + "." + strconv.FormatInt(expires.Unix(), 10)
	token := payload + "." + base64.RawURLEncoding.EncodeToString(s.setupFormSignature(payload))
	http.SetCookie(
		w,
		sessionCookie(
			setupFormCookieName,
			PathSetupPage,
			token,
			r.TLS != nil,
			expires,
		),
	)

	return token, nil
}

func (s *Service) validSetupFormToken(r *http.Request) bool {
	cookie, err := r.Cookie(setupFormCookieName)
	if err != nil {
		return false
	}
	token := r.PostFormValue(setupFormTokenField)
	if !constantTimeMatch(token, cookie.Value) {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !s.now().Before(time.Unix(expiresUnix, 0)) {
		return false
	}
	payload := parts[0] + "." + parts[1]
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}

	return hmac.Equal(providedSignature, s.setupFormSignature(payload))
}

func (s *Service) setupFormSignature(payload string) []byte {
	mac := hmac.New(sha256.New, s.setupFormSigningKey)
	_, _ = mac.Write([]byte(payload))

	return mac.Sum(nil)
}

func clearSetupFormToken(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(
		w,
		clearedSessionCookie(setupFormCookieName, PathSetupPage, r.TLS != nil),
	)
}
