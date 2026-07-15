package adminauth

import "net/http"

func (s *Service) acceptSetupForm(w http.ResponseWriter, r *http.Request) bool {
	release, admitted := acquireAuthRequestAdmission()
	if !admitted {
		writeAuthRequestAdmissionUnavailableHTML(w)

		return false
	}
	defer release()
	boundAuthRequestBody(w, r)
	present, err := s.creds.exists(r.Context())
	if err != nil {
		redirectAuth(w, r, PathSetupPage+"?error=server")

		return false
	}
	if present {
		redirectAuth(w, r, PathLoginPage)

		return false
	}
	if !parseAuthForm(w, r) {
		return false
	}
	validToken := s.validSetupFormToken(r)
	clearSetupFormToken(w, r)
	if !validToken {
		http.Error(w, "missing, invalid, or expired setup token", http.StatusForbidden)

		return false
	}

	return true
}
