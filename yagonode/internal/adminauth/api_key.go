package adminauth

import "encoding/base64"

const (
	apiKeyScheme      = "yago_"
	apiKeyIDBytes     = 12
	apiKeySecretBytes = 32
)

var (
	apiKeyIDLen     = base64.RawURLEncoding.EncodedLen(apiKeyIDBytes)
	apiKeySecretLen = base64.RawURLEncoding.EncodedLen(apiKeySecretBytes)
	apiKeyLen       = len(apiKeyScheme) + apiKeyIDLen + apiKeySecretLen
)

// parseAPIKey splits a presented key into its public identifier and secret. The
// identifier is safe to store and display; the secret is compared against a
// stored hash. Fixed-length base64url fields let the split use offsets so the
// base64url alphabet never collides with a separator.
func parseAPIKey(presented string) (id, secret string, ok bool) {
	if len(presented) != apiKeyLen || presented[:len(apiKeyScheme)] != apiKeyScheme {
		return "", "", false
	}
	body := presented[len(apiKeyScheme):]

	return body[:apiKeyIDLen], body[apiKeyIDLen:], true
}

func formatAPIKey(id, secret string) string {
	return apiKeyScheme + id + secret
}
