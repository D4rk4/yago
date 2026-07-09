package rwi

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

func TestTransferRWIShedsWhenAllIntakeSlotsBusy(t *testing.T) {
	h := openHarness(t, 100, 100)
	gate := httpguard.NewIntakeGate(1)
	if _, ok := gate.TryAcquire(); !ok {
		t.Fatal("occupy the only slot")
	}
	endpoint := transferRWIEndpoint{
		identity: localIdentity(),
		intake:   h.rwi.Receiver,
		gate:     gate,
		accept:   true,
	}

	resp, err := endpoint.Serve(context.Background(), yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultTooHighLoad {
		t.Fatalf("Result = %q, want %q", resp.Result, yagoproto.ResultTooHighLoad)
	}
}
