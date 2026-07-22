package urlmeta

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func (m urlPorts) endpoint() transferURLEndpoint {
	return transferURLEndpoint{
		identity: localIdentity(), intake: m.Receiver,
		senders: acceptingSenderDirectory{}, accept: true,
	}
}

// TestTransferURLRefusesWhenAcceptRemoteIndexOff: with the accept-remote-index
// capability off, a valid transfer is answered error_not_granted (YaCy
// transferURL with allowReceiveIndex disabled) and nothing is stored.
func TestTransferURLRefusesWhenAcceptRemoteIndexOff(t *testing.T) {
	module := openModule(t, 0)
	endpoint := transferURLEndpoint{identity: localIdentity(), intake: module.Receiver}

	resp, err := endpoint.Serve(context.Background(), yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultErrorNotGranted {
		t.Fatalf("Result = %q, want error_not_granted", resp.Result)
	}
}

func TestTransferURLStoresAndAnswers(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.TransferURLResult(yagoproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
}

func TestTransferURLRejectsUnknownSenderBeforeStorage(t *testing.T) {
	module := openModule(t, 0)
	known := yagomodel.Hash("BBBBBBBBBBBB")
	endpoint := module.endpoint()
	endpoint.senders = fixedSenderDirectory{peer: yagomodel.Seed{Hash: known}}

	response, err := endpoint.Serve(t.Context(), yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		Iam:         yagomodel.Hash("CCCCCCCCCCCC"),
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if response.Result != yagoproto.ResultErrorNotGranted {
		t.Fatalf("response = %+v, want error_not_granted", response)
	}
	stored, err := module.Directory.Count(t.Context())
	if err != nil || stored != 0 {
		t.Fatalf("stored rows = %d, %v", stored, err)
	}
}

func TestTransferURLAcceptsKnownInactiveJuniorSender(t *testing.T) {
	module := openModule(t, 0)
	sender := yagomodel.Seed{
		Hash:     yagomodel.Hash("BBBBBBBBBBBB"),
		PeerType: yagomodel.Some(yagomodel.PeerJunior),
	}
	endpoint := module.endpoint()
	endpoint.senders = fixedSenderDirectory{peer: sender}

	response, err := endpoint.Serve(t.Context(), yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		Iam:         sender.Hash,
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	})
	if err != nil || response.Result != yagoproto.ResultOK {
		t.Fatalf("response = %+v, error = %v", response, err)
	}
}

func TestTransferURLRejectsWrongNetwork(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != "" {
		t.Fatalf("Result = %q, want empty auth failure response", resp.Result)
	}
	if len(resp.Encode()) != 0 {
		t.Fatalf("encoded response = %+v, want empty transfer fields", resp.Encode())
	}
}

func TestTransferURLRejectsWrongTargetAfterNetworkAuth(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      yagomodel.Hash("BBBBBBBBBBBB"),
	}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.TransferURLResult(yagoproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
