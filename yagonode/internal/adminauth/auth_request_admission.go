package adminauth

import "net/http"

const maximumConcurrentAuthRequests = 32

var authRequestAdmissionSlots = make(chan struct{}, maximumConcurrentAuthRequests)

func acquireAuthRequestAdmission() (func(), bool) {
	select {
	case authRequestAdmissionSlots <- struct{}{}:
		return func() { <-authRequestAdmissionSlots }, true
	default:
		return nil, false
	}
}

func writeAuthRequestAdmissionUnavailableJSON(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeError(w, http.StatusServiceUnavailable, "authentication capacity exceeded")
}

func writeAuthRequestAdmissionUnavailableHTML(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "authentication capacity exceeded", http.StatusServiceUnavailable)
}
