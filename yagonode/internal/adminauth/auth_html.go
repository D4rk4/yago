package adminauth

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed templates/*.tmpl
var authTemplateFS embed.FS

var authPages = template.Must(template.ParseFS(authTemplateFS, "templates/*.tmpl"))

// Server-rendered auth pages, distinct from the JSON auth API. The login and
// setup pages are unauthenticated (exempt from the guard); logout is a
// CSRF-protected session action.
const (
	PathLoginPage  = "/admin/login"
	PathSetupPage  = "/admin/setup"
	PathLogoutForm = "/admin/logout"

	overviewRedirect = "/admin/overview"
	authHTMLType     = "text/html; charset=utf-8"
	usernameField    = "username"
	passwordField    = "password"
)

type authPageData struct {
	Error  string
	Notice string
}

// CSRFTokenFromContext returns the authenticated session's CSRF token so a
// server-rendered form can carry it into an unsafe request. It is only present
// for cookie-session requests (not API-key requests).
func CSRFTokenFromContext(ctx context.Context) (string, bool) {
	record, ok := sessionFromContext(ctx)
	if !ok || record.CSRFToken == "" {
		return "", false
	}

	return record.CSRFToken, true
}

// MountHTML registers the server-rendered login, first-run setup, and logout
// handlers. Their method+path patterns take precedence over a broader admin
// console subtree handler on the same mux.
func MountHTML(mux *http.ServeMux, service *Service) {
	mux.HandleFunc("GET "+PathLoginPage, service.handleLoginPage)
	mux.HandleFunc("POST "+PathLoginPage, service.handleLoginForm)
	mux.HandleFunc("GET "+PathSetupPage, service.handleSetupPage)
	mux.HandleFunc("POST "+PathSetupPage, service.handleSetupForm)
	mux.HandleFunc("POST "+PathLogoutForm, service.handleLogoutForm)
}

func (s *Service) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if present, err := s.creds.exists(r.Context()); err == nil && !present {
		redirectAuth(w, r, PathSetupPage)

		return
	}
	s.renderAuthPage(w, r, "login", authPageData{
		Error:  loginErrorMessage(r.URL.Query().Get("error")),
		Notice: loginNoticeMessage(r.URL.Query().Get("notice")),
	})
}

func (s *Service) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	username := r.PostFormValue(usernameField)
	password := r.PostFormValue(passwordField)

	caller := clientIP(r)
	if !s.limiter.allow(caller) {
		s.observer.LoginThrottled()
		redirectAuth(w, r, PathLoginPage+"?error=throttled")

		return
	}

	valid, err := s.creds.verify(r.Context(), username, password)
	if err != nil {
		redirectAuth(w, r, PathLoginPage+"?error=server")

		return
	}
	if !valid {
		s.limiter.recordFailure(caller)
		s.observer.LoginFailed()
		redirectAuth(w, r, PathLoginPage+"?error=invalid")

		return
	}
	s.limiter.reset(caller)

	created, err := s.sessions.create(r.Context(), username)
	if err != nil {
		redirectAuth(w, r, PathLoginPage+"?error=server")

		return
	}
	s.observer.LoginSucceeded()
	http.SetCookie(w, sessionCookie(created.Token, r.TLS != nil, created.ExpiresAt))
	redirectAuth(w, r, overviewRedirect)
}

func (s *Service) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if present, err := s.creds.exists(r.Context()); err == nil && present {
		redirectAuth(w, r, PathLoginPage)

		return
	}
	s.renderAuthPage(
		w,
		r,
		"setup",
		authPageData{Error: setupErrorMessage(r.URL.Query().Get("error"))},
	)
}

func (s *Service) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue(usernameField))
	password := r.PostFormValue(passwordField)
	if username == "" || password == "" {
		redirectAuth(w, r, PathSetupPage+"?error=missing")

		return
	}

	err := s.creds.createIfAbsent(r.Context(), username, password)
	if errors.Is(err, errAdminExists) {
		redirectAuth(w, r, PathLoginPage)

		return
	}
	if err != nil {
		redirectAuth(w, r, PathSetupPage+"?error=server")

		return
	}
	redirectAuth(w, r, PathLoginPage+"?notice=created")
}

func (s *Service) handleLogoutForm(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.sessions.delete(r.Context(), cookie.Value)
	}
	http.SetCookie(w, clearedSessionCookie(r.TLS != nil))
	redirectAuth(w, r, PathLoginPage+"?notice=out")
}

func (s *Service) renderAuthPage(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	data authPageData,
) {
	w.Header().Set("Content-Type", authHTMLType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := authPages.ExecuteTemplate(w, name, data); err != nil {
		slog.WarnContext(r.Context(), "auth page render failed", slog.Any("error", err))
	}
}

func redirectAuth(w http.ResponseWriter, r *http.Request, target string) {
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func loginErrorMessage(code string) string {
	switch code {
	case "invalid":
		return "Invalid username or password."
	case "throttled":
		return "Too many attempts. Try again later."
	case "server":
		return "Sign in failed. Please try again."
	default:
		return ""
	}
}

func loginNoticeMessage(code string) string {
	switch code {
	case "created":
		return "Administrator created. Sign in to continue."
	case "out":
		return "You have been signed out."
	default:
		return ""
	}
}

func setupErrorMessage(code string) string {
	switch code {
	case "missing":
		return "Username and password are required."
	case "server":
		return "Setup failed. Please try again."
	default:
		return ""
	}
}
