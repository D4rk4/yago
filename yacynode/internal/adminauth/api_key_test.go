package adminauth

import (
	"strings"
	"testing"
)

func TestParseAPIKeyRoundTrip(t *testing.T) {
	wantID := strings.Repeat("a", apiKeyIDLen)
	wantSecret := strings.Repeat("b", apiKeySecretLen)
	id, secret, ok := parseAPIKey(formatAPIKey(wantID, wantSecret))
	if !ok {
		t.Fatal("parseAPIKey rejected a well-formed key")
	}
	if id != wantID {
		t.Fatalf("id = %q", id)
	}
	if secret != wantSecret {
		t.Fatalf("secret = %q", secret)
	}
}

func TestParseAPIKeyRejectsWrongLength(t *testing.T) {
	if _, _, ok := parseAPIKey(apiKeyScheme + "short"); ok {
		t.Fatal("parseAPIKey accepted a key of the wrong length")
	}
}

func TestParseAPIKeyRejectsWrongScheme(t *testing.T) {
	body := make([]byte, apiKeyIDLen+apiKeySecretLen)
	for i := range body {
		body[i] = 'a'
	}
	if _, _, ok := parseAPIKey("other" + string(body)); ok {
		t.Fatal("parseAPIKey accepted a key without the yago scheme")
	}
}

func TestFormatAPIKeyLength(t *testing.T) {
	created := createKey(t, mustService(t), ScopeAdminRead)
	if len(created.Key) != apiKeyLen {
		t.Fatalf("key length = %d, want %d", len(created.Key), apiKeyLen)
	}
	if _, _, ok := parseAPIKey(created.Key); !ok {
		t.Fatalf("generated key %q does not parse", created.Key)
	}
}

func mustService(t *testing.T) *Service {
	t.Helper()
	service, _ := scriptedService(t)

	return service
}
