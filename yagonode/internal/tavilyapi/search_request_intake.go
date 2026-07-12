package tavilyapi

import (
	"net/http"
)

const maximumConcurrentSearchRequestBodies = 64

func enterSearchRequestIntake(
	w http.ResponseWriter,
	id string,
	intake *requestAdmission,
) (func(), bool) {
	release, admitted := intake.tryEnter()
	if admitted {
		return release, true
	}
	w.Header().Set("Retry-After", "1")
	writeError(
		w,
		http.StatusServiceUnavailable,
		"request_capacity_exceeded",
		"request intake capacity exceeded, try again later",
		id,
	)

	return nil, false
}
