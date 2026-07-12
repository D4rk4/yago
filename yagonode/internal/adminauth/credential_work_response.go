package adminauth

import "net/http"

func writeCredentialWorkUnavailableJSON(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeError(
		w,
		http.StatusServiceUnavailable,
		"authentication capacity exceeded, try again later",
	)
}

func writeCredentialWorkUnavailableHTML(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(
		w,
		"authentication capacity exceeded, try again later",
		http.StatusServiceUnavailable,
	)
}
