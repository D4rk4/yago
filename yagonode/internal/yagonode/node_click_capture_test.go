package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

func TestClickCaptureAdapterRoundTrip(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatalf("clickcapture.Open: %v", err)
	}
	adapter := newClickCaptureAdapter(store)
	candidates := []yacysearch.ImpressionCandidate{
		{URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1},
		{URLIdentity: "https://b.example/", ClusterIdentity: "b", Position: 2},
	}
	prepared, err := adapter.PrepareImpression(t.Context(), "query", candidates)
	if err != nil {
		t.Fatalf("PrepareImpression: %v", err)
	}
	if prepared.Token == "" || len(prepared.Order) != 2 {
		t.Fatalf("prepared = %#v", prepared)
	}
	original := prepared.Order[0]
	if err := adapter.RecordClick(
		t.Context(),
		prepared.Token,
		candidates[original].URLIdentity,
		1,
	); err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if err := adapter.RecordClick(
		t.Context(),
		prepared.Token,
		candidates[original].URLIdentity,
		1,
	); err == nil {
		t.Fatal("replayed click succeeded")
	}
}

func TestClickCaptureAdapterNilAndErrors(t *testing.T) {
	if newClickCaptureAdapter(nil) != nil {
		t.Fatal("nil store produced an adapter")
	}
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatalf("clickcapture.Open: %v", err)
	}
	adapter := newClickCaptureAdapter(store)
	if _, err := adapter.PrepareImpression(t.Context(), "", nil); err == nil {
		t.Fatal("invalid impression succeeded")
	}
	if err := adapter.RecordClick(t.Context(), "invalid", "identity", 1); err == nil {
		t.Fatal("invalid click succeeded")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := adapter.PrepareImpression(canceled, "query", []yacysearch.ImpressionCandidate{{
		URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
	}}); err == nil {
		t.Fatal("canceled impression succeeded")
	}
}
