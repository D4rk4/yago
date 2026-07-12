package adminauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maximumAuthRequestBodyBytes int64 = 16 << 10
	maximumAdminUsernameBytes         = 256
	maximumAdminPasswordBytes         = 1 << 10
)

var (
	errCredentialsRequired = errors.New("username and password are required")
	errCredentialsTooLong  = errors.New("username or password is too long")
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func boundAuthRequestBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maximumAuthRequestBodyBytes)
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentialsRequest, bool) {
	req, err := decodeCredentialsRequest(r)
	if err == nil {
		return req, true
	}
	if isAuthRequestTooLarge(err) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")

		return credentialsRequest{}, false
	}
	if errors.Is(err, errCredentialsRequired) || errors.Is(err, errCredentialsTooLong) {
		writeError(w, http.StatusBadRequest, err.Error())

		return credentialsRequest{}, false
	}
	writeError(w, http.StatusBadRequest, "invalid request body")

	return credentialsRequest{}, false
}

func decodeCredentialsRequest(r *http.Request) (credentialsRequest, error) {
	if r.ContentLength > maximumAuthRequestBodyBytes {
		return credentialsRequest{}, &http.MaxBytesError{Limit: maximumAuthRequestBodyBytes}
	}
	decoder := json.NewDecoder(r.Body)
	var req credentialsRequest
	if err := decoder.Decode(&req); err != nil {
		return credentialsRequest{}, fmt.Errorf("decode credentials: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return credentialsRequest{}, fmt.Errorf("credentials have trailing data")
	}
	if err := validateCredentials(req); err != nil {
		return credentialsRequest{}, err
	}

	return req, nil
}

func parseAuthForm(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength > maximumAuthRequestBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)

		return false
	}
	if err := r.ParseForm(); err != nil {
		if isAuthRequestTooLarge(err) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request body", http.StatusBadRequest)
		}

		return false
	}

	return true
}

func credentialsFromForm(r *http.Request, trimUsername bool) (credentialsRequest, error) {
	username := r.PostForm.Get(usernameField)
	if trimUsername {
		username = strings.TrimSpace(username)
	}
	req := credentialsRequest{
		Username: username,
		Password: r.PostForm.Get(passwordField),
	}

	return req, validateCredentials(req)
}

func validateCredentials(req credentialsRequest) error {
	if req.Username == "" || req.Password == "" {
		return errCredentialsRequired
	}
	if len(req.Username) > maximumAdminUsernameBytes ||
		len(req.Password) > maximumAdminPasswordBytes {
		return errCredentialsTooLong
	}

	return nil
}

func isAuthRequestTooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)

	return ok
}
