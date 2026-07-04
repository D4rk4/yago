package peermessage

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

type Message struct {
	ReceivedAt time.Time
	FromName   string
	FromHash   yagomodel.Hash
	ToName     string
	ToHash     yagomodel.Hash
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
		yagoproto.PathMessage,
		yagoproto.MessageEndpointMethods,
		yagoproto.ParseMessageRequest,
		endpoint{identity: identity, inbox: noInbox(inbox)}.Serve,
	)
}

func senderName(seed yagomodel.Seed) string {
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
