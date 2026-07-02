package peermessage

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/vault"
	"github.com/D4rk4/yago/yacyproto"
)

type Message struct {
	ReceivedAt time.Time
	FromName   string
	FromHash   yacymodel.Hash
	ToName     string
	ToHash     yacymodel.Hash
	Subject    string
	Body       string
}

type Inbox interface {
	Receive(ctx context.Context, message Message) error
}

func OpenMailbox(v *vault.Vault, now func() time.Time) (*Mailbox, error) {
	messages, err := registerMessages(v)
	if err != nil {
		return nil, err
	}

	return &Mailbox{vault: v, messages: messages, now: now}, nil
}

func Mount(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	inbox Inbox,
) {
	httpguard.Mount(
		router,
		yacyproto.PathMessage,
		yacyproto.MessageEndpointMethods,
		yacyproto.ParseMessageRequest,
		endpoint{identity: identity, inbox: noInbox(inbox)}.Serve,
	)
}

func senderName(seed yacymodel.Seed) string {
	name, ok := seed.Name.Get()
	if !ok || name == "" {
		return "anonymous"
	}

	return name
}

func wrapReceiveMessage(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("receive peer message: %w", err)
}
