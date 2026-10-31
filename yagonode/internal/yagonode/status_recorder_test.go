package yagonode

import (
	"net/http/httptest"
	"testing"
)

func TestStatusRecorderUnwrapReturnsUnderlyingWriter(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	if got := recorder.Unwrap(); got != underlying {
		t.Fatalf("unwrapped writer=%T, want %T", got, underlying)
	}
}
