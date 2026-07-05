package documentsearch

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

func TestInboundSearchShedsWhenAllSlotsBusy(t *testing.T) {
	gate := httpguard.NewIntakeGate(1)
	if _, ok := gate.TryAcquire(); !ok {
		t.Fatal("occupy the only slot")
	}
	endpoint := searchEndpoint{identity: searchIdentity(), searcher: searcher{}, gate: gate}

	resp, err := endpoint.Serve(t.Context(), yagoproto.SearchRequest{
		NetworkName: "freeworld",
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.JoinCount != 0 || resp.Count != 0 {
		t.Fatalf("shed response = %#v, want empty", resp)
	}
}
