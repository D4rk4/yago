package infrastructure

import (
	"expvar"
	"net/http"
	"strconv"
)

const MetricHTTPResponses = "http_responses"

func InstrumentHTTP(next http.Handler) http.Handler {
	responses := httpResponsesMap()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		responses.Add(strconv.Itoa(recorder.status), 1)
	})
}

func httpResponsesMap() *expvar.Map {
	if existing, ok := expvar.Get(MetricHTTPResponses).(*expvar.Map); ok {
		return existing
	}

	return expvar.NewMap(MetricHTTPResponses)
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}
