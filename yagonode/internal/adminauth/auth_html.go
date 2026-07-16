package adminauth

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/*.tmpl assets/auth.css
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
	Error           string
	Notice          string
	SetupToken      string
	Stylesheet      string
	LoginNodeStatus LoginNodeStatus
	// Wizard arms the node-mode step of the first-run setup page.
	Wizard   bool
	Defaults SetupDefaults
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
	mux.Handle("GET "+PathLoginPage, withAuthPagePolicy(http.HandlerFunc(service.handleLoginPage)))
	mux.Handle("POST "+PathLoginPage, withAuthPagePolicy(http.HandlerFunc(service.handleLoginForm)))
	mux.Handle("GET "+PathSetupPage, withAuthPagePolicy(http.HandlerFunc(service.handleSetupPage)))
	mux.Handle("POST "+PathSetupPage, withAuthPagePolicy(http.HandlerFunc(service.handleSetupForm)))
	mux.Handle(
		"POST "+PathLogoutForm,
		withAuthPagePolicy(http.HandlerFunc(service.handleLogoutForm)),
	)
	mux.HandleFunc("GET "+PathAuthStylesheet, serveAuthStylesheet)
}

func (s *Service) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if present, err := s.creds.exists(r.Context()); err == nil && !present {
		redirectAuth(w, r, PathSetupPage)

		return
	}
	s.renderAuthPage(w, r, "login", authPageData{
		Error:           loginErrorMessage(r.URL.Query().Get("error")),
		Notice:          loginNoticeMessage(r.URL.Query().Get("notice")),
		LoginNodeStatus: normalizedLoginNodeStatus(r.Context(), s.loginNodeStatus),
	})
}

func (s *Service) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	caller := clientIP(r)
	if !s.limiter.allow(caller) {
		s.observer.LoginThrottled()
		redirectAuth(w, r, PathLoginPage+"?error=throttled")

		return
	}
	release, admitted := acquireAuthRequestAdmission()
	if !admitted {
		writeAuthRequestAdmissionUnavailableHTML(w)

		return
	}
	boundAuthRequestBody(w, r)
	parsed := func() bool {
		defer release()

		return parseAuthForm(w, r)
	}()
	if !parsed {
		s.limiter.recordFailure(caller)
		s.observer.LoginFailed()

		return
	}
	credentials, err := credentialsFromForm(r, false)
	if err != nil {
		s.limiter.recordFailure(caller)
		s.observer.LoginFailed()
		redirectAuth(w, r, PathLoginPage+"?error=invalid")

		return
	}

	valid, err := s.creds.verify(
		r.Context(),
		credentials.Username,
		credentials.Password,
	)
	if errors.Is(err, errCredentialWorkUnavailable) {
		writeCredentialWorkUnavailableHTML(w)

		return
	}
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

	created, err := s.sessions.create(r.Context(), credentials.Username)
	if err != nil {
		redirectAuth(w, r, PathLoginPage+"?error=server")

		return
	}
	s.observer.LoginSucceeded()
	http.SetCookie(
		w,
		sessionCookie(sessionCookieName, "/", created.Token, r.TLS != nil, created.ExpiresAt),
	)
	redirectAuth(w, r, overviewRedirect)
}

func (s *Service) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if present, err := s.creds.exists(r.Context()); err == nil && present {
		redirectAuth(w, r, PathLoginPage)

		return
	}
	setupToken, err := s.issueSetupFormToken(w, r)
	if err != nil {
		http.Error(w, "setup is temporarily unavailable", http.StatusServiceUnavailable)

		return
	}
	s.renderAuthPage(
		w,
		r,
		"setup",
		authPageData{
			Error:      setupErrorMessage(r.URL.Query().Get("error")),
			SetupToken: setupToken,
			Wizard:     s.wizardApply != nil,
			Defaults:   s.wizardDefaults,
		},
	)
}

func (s *Service) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if !s.acceptSetupForm(w, r) {
		return
	}
	credentials, err := credentialsFromForm(r, true)
	if errors.Is(err, errCredentialsRequired) {
		redirectAuth(w, r, PathSetupPage+"?error=missing")

		return
	}
	if err != nil {
		redirectAuth(w, r, PathSetupPage+"?error=invalid")

		return
	}

	err = s.creds.createIfAbsent(
		r.Context(),
		credentials.Username,
		credentials.Password,
	)
	if errors.Is(err, errCredentialWorkUnavailable) {
		writeCredentialWorkUnavailableHTML(w)

		return
	}
	if errors.Is(err, errAdminExists) {
		redirectAuth(w, r, PathLoginPage)

		return
	}
	if err != nil {
		redirectAuth(w, r, PathSetupPage+"?error=server")

		return
	}
	if s.wizardApply != nil {
		if err := s.wizardApply(r.Context(), wizardChoices(r.PostForm.Get)); err != nil {
			// The administrator exists; the node-mode settings can be redone
			// in the console, so surface the partial success honestly.
			redirectAuth(w, r, PathLoginPage+"?notice=created&error=wizard")

			return
		}
		if s.wizardRestart != nil {
			// Several wizard choices only take effect at boot, so setup ends in
			// a mandatory restart: render the notice first, then trigger —
			// graceful shutdown waits for this response to finish.
			s.renderAuthPage(w, r, "restarting", authPageData{})
			s.wizardRestart()

			return
		}
	}
	redirectAuth(w, r, PathLoginPage+"?notice=created")
}

func (s *Service) handleLogoutForm(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.sessions.delete(r.Context(), cookie.Value)
	}
	http.SetCookie(w, clearedSessionCookie(sessionCookieName, "/", r.TLS != nil))
	redirectAuth(w, r, PathLoginPage)
}

func (s *Service) renderAuthPage(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	data authPageData,
) {
	w.Header().Set("Content-Type", authHTMLType)
	data.Stylesheet = authStylesheetReference

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
	default:
		return ""
	}
}

func setupErrorMessage(code string) string {
	switch code {
	case "missing":
		return "Username and password are required."
	case "invalid":
		return "Username or password is too long."
	case "server":
		return "Setup failed. Please try again."
	default:
		return ""
	}
}
