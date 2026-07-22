package peermessage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
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
	}.Serve(t.Context(), yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yagoproto.MessageProcessPermission,
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
	if resp.Response != yagoproto.MessageResponsePermission {
		t.Fatalf("Response = %q, want permission", resp.Response)
	}
}

func TestMessageRejectsWrongTarget(t *testing.T) {
	inbox := &recordingInbox{}
	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    inbox,
	}.Serve(t.Context(), yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("other"),
		Process:     yagoproto.MessageProcessPermission,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Response != yagoproto.MessageResponseRejected {
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
	}.Serve(t.Context(), yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yagoproto.MessageProcessPost,
		MySeed:      yagomodel.Some(senderSeed()),
		Subject:     "  hello  ",
		Body:        "  body  ",
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Response != yagoproto.MessageResponseAccepted {
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
	seed.Name = yagomodel.None[string]()

	resp, err := endpoint{
		identity: localIdentity(),
		inbox:    inbox,
	}.Serve(t.Context(), yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yagoproto.MessageProcessPost,
		MySeed:      yagomodel.Some(seed),
		Subject:     "hello",
		Body:        "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response != yagoproto.MessageResponseAccepted {
		t.Fatalf("Response = %q, want accepted", resp.Response)
	}
	if inbox.message.FromName != "anonymous" {
		t.Fatalf("FromName = %q, want anonymous", inbox.message.FromName)
	}
}

func TestMessagePostRejectsMissingSeedOrEmptyContent(t *testing.T) {
	inbox := &recordingInbox{}
	endpoint := endpoint{identity: localIdentity(), inbox: inbox}

	for _, req := range []yagoproto.MessageRequest{
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yagoproto.MessageProcessPost, Subject: "hello", Body: "body"},
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yagoproto.MessageProcessPost, MySeed: yagomodel.Some(senderSeed()), Subject: "", Body: "body"},
		{NetworkName: "freeworld", YouAre: hashFor("self"), Process: yagoproto.MessageProcessPost, MySeed: yagomodel.Some(senderSeed()), Subject: "hello", Body: ""},
	} {
		resp, err := endpoint.Serve(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Response != yagoproto.MessageResponseRejected {
			t.Fatalf("Response = %q, want rejected", resp.Response)
		}
	}

	if inbox.called {
		t.Fatal("inbox called for rejected message")
	}
}

func TestMessagePostEnforcesDecodedContentLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		subject string
		body    string
		want    string
	}{
		{name: "exact subject", subject: strings.Repeat("s", acceptedSubjectSize), body: "body", want: yagoproto.MessageResponseAccepted},
		{name: "long subject", subject: strings.Repeat("s", acceptedSubjectSize+1), body: "body", want: yagoproto.MessageResponseRejected},
		{name: "subject limit plus space", subject: strings.Repeat("s", acceptedSubjectSize) + " ", body: "body", want: yagoproto.MessageResponseRejected},
		{name: "exact body", subject: "subject", body: strings.Repeat("b", acceptedMessageSize), want: yagoproto.MessageResponseAccepted},
		{name: "long body", subject: "subject", body: strings.Repeat("b", acceptedMessageSize+1), want: yagoproto.MessageResponseRejected},
		{name: "body limit plus space", subject: "subject", body: strings.Repeat("b", acceptedMessageSize) + " ", want: yagoproto.MessageResponseRejected},
		{name: "blank subject", subject: " \t ", body: "body", want: yagoproto.MessageResponseRejected},
		{name: "blank body", subject: "subject", body: " \n ", want: yagoproto.MessageResponseRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			inbox := &recordingInbox{}
			resp, err := endpoint{identity: localIdentity(), inbox: inbox}.Serve(
				t.Context(),
				yagoproto.MessageRequest{
					NetworkName: "freeworld", YouAre: hashFor("self"),
					Process: yagoproto.MessageProcessPost, MySeed: yagomodel.Some(senderSeed()),
					Subject: test.subject, Body: test.body,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Response != test.want ||
				inbox.called != (test.want == yagoproto.MessageResponseAccepted) {
				t.Fatalf(
					"response = %q inbox=%t, want %q inbox=%t",
					resp.Response,
					inbox.called,
					test.want,
					test.want == yagoproto.MessageResponseAccepted,
				)
			}
		})
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
	}.Serve(t.Context(), yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yagoproto.MessageProcessPost,
		MySeed:      yagomodel.Some(senderSeed()),
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
	req := yagoproto.MessageRequest{
		NetworkName: "freeworld",
		YouAre:      hashFor("self"),
		Process:     yagoproto.MessageProcessPermission,
	}
	form := req.Form()
	form.Set(yagoproto.FieldIam, "short")
	form.Set(yagoproto.FieldMySeed, yagomodel.EncodeCompactWireForm("{Hash=short}"))
	form.Set(yagoproto.FieldMessageSubject, "z|@@@")
	form.Set(yagoproto.FieldMessage, "z|@@@")

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathMessage+"?"+form.Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	if msg[yagoproto.FieldResponse] != yagoproto.MessageResponsePermission {
		t.Fatalf("response = %q, want permission", msg[yagoproto.FieldResponse])
	}
}

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        hashFor("self"),
		NetworkName: "freeworld",
		Name:        "self-peer",
	}
}

func senderSeed() yagomodel.Seed {
	return yagomodel.Seed{
		Hash: hashFor("sender"),
		Name: yagomodel.Some("sender-peer"),
	}
}

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yagomodel.Hash {
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + hashFiller[len(base):])
}
