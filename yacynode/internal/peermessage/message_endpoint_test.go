package peermessage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type recordingInbox struct {
	message Message
	called  bool
	err     error
}

func (r *recordingInbox) Receive(_ context.Context, message Message) error {
	r.called = true
	r.message = message

	return r.err
}

func TestMessagePermissionAcceptedForAddressedPeer(t *testing.T) {
	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    &recordingInbox{},
	}.Serve(t.Context(), yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yacyproto.MessageProcessPermission,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.MessageSize != acceptedMessageSize {
		t.Fatalf("MessageSize = %d, want %d", resp.MessageSize, acceptedMessageSize)
	}
	if resp.AttachmentSize != acceptedAttachmentSize {
		t.Fatalf("AttachmentSize = %d, want %d", resp.AttachmentSize, acceptedAttachmentSize)
	}
	if resp.Response != yacyproto.MessageResponsePermission {
		t.Fatalf("Response = %q, want permission", resp.Response)
	}
}

func TestMessageRejectsWrongTarget(t *testing.T) {
	inbox := &recordingInbox{}
	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    inbox,
	}.Serve(t.Context(), yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("other"),
		Process:     yacyproto.MessageProcessPermission,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Response != yacyproto.MessageResponseRejected {
		t.Fatalf("Response = %q, want rejected", resp.Response)
	}
	if resp.MessageSize != 0 || resp.AttachmentSize != 0 {
		t.Fatalf("sizes = %d/%d, want zero", resp.MessageSize, resp.AttachmentSize)
	}
	if inbox.called {
		t.Fatal("inbox called for wrong target")
	}
}

func TestMessageStoresPostedMessage(t *testing.T) {
	inbox := &recordingInbox{}
	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    inbox,
	}.Serve(t.Context(), yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yacyproto.MessageProcessPost,
		MySeed:      yacymodel.Some(senderSeed()),
		Subject:     "  hello  ",
		Body:        "  body  ",
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Response != yacyproto.MessageResponseAccepted {
		t.Fatalf("Response = %q, want accepted", resp.Response)
	}
	if !inbox.called {
		t.Fatal("inbox was not called")
	}
	if inbox.message.FromName != "sender-peer" || inbox.message.FromHash != hashFor("sender") {
		t.Fatalf("sender = %q/%q", inbox.message.FromName, inbox.message.FromHash)
	}
	if inbox.message.ToName != "self-peer" || inbox.message.ToHash != hashFor("self") {
		t.Fatalf("recipient = %q/%q", inbox.message.ToName, inbox.message.ToHash)
	}
	if inbox.message.Subject != "hello" || inbox.message.Body != "body" {
		t.Fatalf("message = %q/%q", inbox.message.Subject, inbox.message.Body)
	}
}

func TestMessageUsesAnonymousSenderNameWhenSeedHasNoName(t *testing.T) {
	inbox := &recordingInbox{}
	seed := senderSeed()
	seed.Name = yacymodel.None[string]()

	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    inbox,
	}.Serve(t.Context(), yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yacyproto.MessageProcessPost,
		MySeed:      yacymodel.Some(seed),
		Subject:     "hello",
		Body:        "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response != yacyproto.MessageResponseAccepted {
		t.Fatalf("Response = %q, want accepted", resp.Response)
	}
	if inbox.message.FromName != "anonymous" {
		t.Fatalf("FromName = %q, want anonymous", inbox.message.FromName)
	}
}

func TestMessagePostRejectsMissingSeedOrEmptyContent(t *testing.T) {
	inbox := &recordingInbox{}
	endpoint := endpoint{identity: localIdentity(), inbox: inbox}

	for _, req := range []yacyproto.MessageRequest{
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yacyproto.MessageProcessPost, Subject: "hello", Body: "body"},
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yacyproto.MessageProcessPost, MySeed: yacymodel.Some(senderSeed()), Subject: "", Body: "body"},
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yacyproto.MessageProcessPost, MySeed: yacymodel.Some(senderSeed()), Subject: "hello", Body: ""},
	} {
		resp, err := endpoint.Serve(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Response != yacyproto.MessageResponseRejected {
			t.Fatalf("Response = %q, want rejected", resp.Response)
		}
	}

	if inbox.called {
		t.Fatal("inbox called for rejected message")
	}
}

func TestNoInboxRejectsReceive(t *testing.T) {
	err := noInbox(nil).Receive(t.Context(), Message{})
	if err == nil {
		t.Fatal("expected unavailable inbox error")
	}
}

func TestNoInboxKeepsProvidedInbox(t *testing.T) {
	inbox := &recordingInbox{}
	if err := noInbox(inbox).Receive(t.Context(), Message{Subject: "hello"}); err != nil {
		t.Fatal(err)
	}
	if !inbox.called {
		t.Fatal("provided inbox was not called")
	}
}

func TestMessageSurfacesInboxFailure(t *testing.T) {
	want := errors.New("disk full")
	_, err := endpoint{
		identity: localIdentity(),
		inbox:    &recordingInbox{err: want},
	}.Serve(t.Context(), yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yacyproto.MessageProcessPost,
		MySeed:      yacymodel.Some(senderSeed()),
		Subject:     "hello",
		Body:        "body",
	})

	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

type messageStatus struct{}

func (messageStatus) Version(context.Context) string { return "1.940" }
func (messageStatus) Uptime(context.Context) int     { return 42 }

func messageGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(4096, time.Second),
		Respond: httpguard.NewWireResponder(messageStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestMountServesMessageRoute(t *testing.T) {
	mux := http.NewServeMux()
	Mount(httpguard.NewWireRouter(mux, messageGate()), localIdentity(), nil)
	req := yacyproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yacyproto.MessageProcessPermission,
	}
	form := req.Form()
	form.Set(yacyproto.FieldIam, "short")
	form.Set(yacyproto.FieldMySeed, yacymodel.EncodeCompactWireForm("{Hash=short}"))
	form.Set(yacyproto.FieldMessageSubject, "z|@@@")
	form.Set(yacyproto.FieldMessage, "z|@@@")

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathMessage+"?"+form.Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	msg, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	if msg[yacyproto.FieldResponse] != yacyproto.MessageResponsePermission {
		t.Fatalf("response = %q, want permission", msg[yacyproto.FieldResponse])
	}
}

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        hashFor("self"),
		NetworkName: "freeworld",
		Name:        "self-peer",
	}
}

func senderSeed() yacymodel.Seed {
	return yacymodel.Seed{
		Hash: hashFor("sender"),
		Name: yacymodel.Some("sender-peer"),
	}
}

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yacymodel.Hash {
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}
