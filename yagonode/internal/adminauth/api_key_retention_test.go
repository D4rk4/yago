package adminauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAPIKeyStoreRejectsCreationAtCapacity(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	for range maximumAPIKeys {
		if _, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead}); err != nil {
			t.Fatalf("create before capacity: %v", err)
		}
	}
	if _, err := store.create(
		context.Background(),
		"overflow",
		[]Scope{ScopeAdminRead},
	); !errors.Is(err, errAPIKeyCapacityReached) {
		t.Fatalf("overflow error = %v", err)
	}
	keys, err := store.list(context.Background())
	if err != nil || len(keys) != maximumAPIKeys {
		t.Fatalf("keys at capacity = %d, %v", len(keys), err)
	}
}

func TestAPIKeyEndpointReportsCapacityToOperator(t *testing.T) {
	service := testService(t)
	for range maximumAPIKeys {
		if _, err := service.apiKeys.create(
			context.Background(),
			"ci",
			[]Scope{ScopeAdminRead},
		); err != nil {
			t.Fatalf("create before capacity: %v", err)
		}
	}
	recorder := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathAPIKeys,
		`{"label":"overflow","scopes":["admin:read"]}`,
	)
	if recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), keyCapacityOperatorMessage) {
		t.Fatalf("capacity response = %d, %s", recorder.Code, recorder.Body.String())
	}
}

func TestServiceCreateAPIKeyPreservesCapacityCause(t *testing.T) {
	service := testService(t)
	for range maximumAPIKeys {
		if _, err := service.apiKeys.create(
			context.Background(),
			"ci",
			[]Scope{ScopeAdminRead},
		); err != nil {
			t.Fatalf("create before capacity: %v", err)
		}
	}
	_, err := service.CreateAPIKey(context.Background(), "overflow", []string{"admin:read"})
	if !errors.Is(err, errAPIKeyCapacityReached) {
		t.Fatalf("capacity cause = %v", err)
	}
}

func TestAPIKeyCapacityOperatorMessageClassifiesOnlyCapacity(t *testing.T) {
	wrapped := errors.New("different failure")
	if message, classified := APIKeyCapacityOperatorMessage(wrapped); classified || message != "" {
		t.Fatalf("different failure classified as capacity: %q", message)
	}
	message, classified := APIKeyCapacityOperatorMessage(
		errors.Join(errors.New("create failed"), errAPIKeyCapacityReached),
	)
	if !classified || message != keyCapacityOperatorMessage {
		t.Fatalf("capacity classification = %q, %t", message, classified)
	}
}
