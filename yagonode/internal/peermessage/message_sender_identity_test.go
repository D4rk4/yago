package peermessage

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestMessagePostBindsSenderToSeed(t *testing.T) {
	seed := senderSeed()
	for _, test := range []struct {
		name string
		iam  yagomodel.Hash
		want string
	}{
		{"matching", seed.Hash, yagoproto.MessageResponseAccepted},
		{"different", hashFor("different"), yagoproto.MessageResponseRejected},
		{"missing", "", yagoproto.MessageResponseRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			inbox := &recordingInbox{}
			response, err := (endpoint{identity: localIdentity(), inbox: inbox}).Serve(
				t.Context(), yagoproto.MessageRequest{
					NetworkName: "freeworld", YouAre: hashFor("self"), Iam: test.iam,
					Process: yagoproto.MessageProcessPost, MySeed: yagomodel.Some(seed),
					Subject: "subject", Body: "body",
				})
			if err != nil || response.Response != test.want {
				t.Fatalf("response=%+v error=%v", response, err)
			}
			accepted := test.want == yagoproto.MessageResponseAccepted
			if inbox.called != accepted {
				t.Fatalf("inbox called=%v, want %v", inbox.called, accepted)
			}
			if accepted && (inbox.message.FromHash != seed.Hash ||
				inbox.message.Subject != "subject" || inbox.message.Body != "body") {
				t.Fatalf("accepted message changed: %+v", inbox.message)
			}
		})
	}
}
