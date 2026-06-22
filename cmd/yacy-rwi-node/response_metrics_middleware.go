package main

import (
	"expvar"
	"net/http"
	"strconv"
)

const metricHTTPResponses = "http_responses"

func instrumentHTTP(next http.Handler) http.Handler {
	responses := httpResponsesMap()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		responses.Add(strconv.Itoa(recorder.status), 1)
	})
}

func httpResponsesMap() *expvar.Map {
	if existing, ok := expvar.Get(metricHTTPResponses).(*expvar.Map); ok {
		return existing
	}

	return expvar.NewMap(metricHTTPResponses)
}
