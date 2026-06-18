package infrastructure

import (
	"context"
	"expvar"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestInstrumentHTTPRecordsStatus(t *testing.T) {
	handler := InstrumentHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	before := responseCount(t, http.StatusTeapot)
	serve(handler)

	if got := responseCount(t, http.StatusTeapot); got != before+1 {
		t.Errorf("count = %d, want %d", got, before+1)
	}
}

func TestInstrumentHTTPDefaultsToOK(t *testing.T) {
	handler := InstrumentHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	before := responseCount(t, http.StatusOK)
	serve(handler)

	if got := responseCount(t, http.StatusOK); got != before+1 {
		t.Errorf("count = %d, want %d", got, before+1)
	}
}

func serve(handler http.Handler) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
}

func responseCount(t *testing.T, status int) int64 {
	t.Helper()

	responses, ok := expvar.Get(MetricHTTPResponses).(*expvar.Map)
	if !ok {
		return 0
	}
	value, ok := responses.Get(strconv.Itoa(status)).(*expvar.Int)
	if !ok {
		return 0
	}

	return value.Value()
}
