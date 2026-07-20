package yagonode

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestScopedTavilyCorruptCredentialMapsToServiceUnavailable(t *testing.T) {
	engine := newCtrlEngine()
	storage := ctrlVault(t, engine)
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("open auth service: %v", err)
	}
	created, err := service.CreateAPIKey(t.Context(), "corruption probe", []string{"search:read"})
	if err != nil {
		t.Fatalf("mint credential: %v", err)
	}
	bucket := engine.bucket(vault.Name("adminauth_api_keys"))
	var record map[string]any
	if err := json.Unmarshal(bucket[created.ID], &record); err != nil {
		t.Fatalf("decode stored record: %v", err)
	}
	secretHash, ok := record["secretHash"].(string)
	if !ok || len(secretHash) == 0 {
		t.Fatal("stored record omitted its credential hash")
	}
	record["secretHash"] = "A" + secretHash[1:]
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode corrupted record: %v", err)
	}
	bucket[created.ID] = encoded

	assertScopedTavilyStatus(t, scopedTavilyHandler(service), scopedTavilyProbe{
		Path:       tavilyapi.PathSearch,
		Body:       scopedBasicSearchBody,
		Credential: created.Secret,
		Status:     http.StatusServiceUnavailable,
	})
}
