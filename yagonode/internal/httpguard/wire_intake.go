package httpguard

import "net/http"

func enterWireIntake(
	w http.ResponseWriter,
	intake *IntakeGate,
) (func(), bool) {
	release, admitted := intake.TryAcquire()
	if admitted {
		return release, true
	}
	w.Header().Set("Retry-After", "1")
	http.Error(w, "wire request capacity exceeded", http.StatusServiceUnavailable)

	return nil, false
}
