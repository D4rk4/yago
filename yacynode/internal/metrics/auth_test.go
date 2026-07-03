package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAuthMetricsCountsLoginOutcomes(t *testing.T) {
	observer := NewAuthMetrics(prometheus.NewRegistry())

	observer.LoginSucceeded()
	observer.LoginFailed()
	observer.LoginFailed()
	observer.LoginThrottled()

	if got := testutil.ToFloat64(observer.logins.WithLabelValues("success")); got != 1 {
		t.Fatalf("login success = %v, want 1", got)
	}
	if got := testutil.ToFloat64(observer.logins.WithLabelValues("failure")); got != 2 {
		t.Fatalf("login failure = %v, want 2", got)
	}
	if got := testutil.ToFloat64(observer.logins.WithLabelValues("throttled")); got != 1 {
		t.Fatalf("login throttled = %v, want 1", got)
	}
}

func TestAuthMetricsCountsAPIKeyOutcomes(t *testing.T) {
	observer := NewAuthMetrics(prometheus.NewRegistry())

	observer.APIKeyRejected()
	observer.APIKeyThrottled()
	observer.APIKeyForbidden()
	observer.APIKeyForbidden()

	if got := testutil.ToFloat64(observer.apiKeys.WithLabelValues("rejected")); got != 1 {
		t.Fatalf("api key rejected = %v, want 1", got)
	}
	if got := testutil.ToFloat64(observer.apiKeys.WithLabelValues("throttled")); got != 1 {
		t.Fatalf("api key throttled = %v, want 1", got)
	}
	if got := testutil.ToFloat64(observer.apiKeys.WithLabelValues("forbidden")); got != 2 {
		t.Fatalf("api key forbidden = %v, want 2", got)
	}
}
