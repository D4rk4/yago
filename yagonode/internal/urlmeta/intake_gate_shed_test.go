package urlmeta

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

func TestTransferURLShedsWhenAllIntakeSlotsBusy(t *testing.T) {
	gate := httpguard.NewIntakeGate(1)
	if _, ok := gate.TryAcquire(); !ok {
		t.Fatal("occupy the only slot")
	}
	endpoint := transferURLEndpoint{
		identity: localIdentity(),
		intake:   okURLReceiver{},
		gate:     gate,
	}

	resp, err := endpoint.Serve(t.Context(), yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultErrorNotGranted {
		t.Fatalf("Result = %q, want %q", resp.Result, yagoproto.ResultErrorNotGranted)
	}
}
