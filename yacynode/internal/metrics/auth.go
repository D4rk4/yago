package metrics

import "github.com/prometheus/client_golang/prometheus"

const labelResult = "result"

// AuthMetrics counts admin authentication outcomes so operators can watch login
// pressure and API-key rejections. Its method set satisfies the observer the
// admin auth service calls, so it can be passed there directly.
type AuthMetrics struct {
	logins  *prometheus.CounterVec
	apiKeys *prometheus.CounterVec
}

func NewAuthMetrics(registry prometheus.Registerer) *AuthMetrics {
	logins := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "admin_login_attempts_total",
			Help: "Admin login attempts, by result (success, failure, throttled).",
		},
		[]string{labelResult},
	)
	apiKeys := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "admin_api_key_auth_total",
			Help: "Admin API key authentications, by result (rejected, throttled, forbidden).",
		},
		[]string{labelResult},
	)
	registry.MustRegister(logins, apiKeys)

	return &AuthMetrics{logins: logins, apiKeys: apiKeys}
}

func (m *AuthMetrics) LoginSucceeded() { m.logins.WithLabelValues("success").Inc() }

func (m *AuthMetrics) LoginFailed() { m.logins.WithLabelValues("failure").Inc() }

func (m *AuthMetrics) LoginThrottled() { m.logins.WithLabelValues("throttled").Inc() }

func (m *AuthMetrics) APIKeyRejected() { m.apiKeys.WithLabelValues("rejected").Inc() }

func (m *AuthMetrics) APIKeyThrottled() { m.apiKeys.WithLabelValues("throttled").Inc() }

func (m *AuthMetrics) APIKeyForbidden() { m.apiKeys.WithLabelValues("forbidden").Inc() }
