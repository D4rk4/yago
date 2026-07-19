package peermessage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

// acceptedMessageSize and acceptedAttachmentSize mirror YaCy message.java's
// hardcoded advertised limits (messagesize=10240, attachmentsize=0): upstream
// declines attachments and does not require the sender's iam hash (it is
// commented out there), so accepting a decoded text subject and body from a
// network-matched, correctly addressed peer is full parity, not a narrowing
// (verified against source/net/yacy/htroot/yacy/message.java, 2026-07).
const (
	acceptedMessageSize    = 10240
	acceptedAttachmentSize = 0
)

type endpoint struct {
	identity nodeidentity.Identity
	inbox    Inbox
}

func (e endpoint) Serve(
	ctx context.Context,
	req yagoproto.MessageRequest,
) (yagoproto.MessageResponse, error) {
	resp := rejectedResponse()
	if !e.identity.AuthenticatesAddress(
		req.NetworkName,
		req.YouAre,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) {
		return resp, nil
	}

	resp.MessageSize = acceptedMessageSize
	resp.AttachmentSize = acceptedAttachmentSize

	switch req.Process {
	case yagoproto.MessageProcessPermission:
		resp.Response = yagoproto.MessageResponsePermission
	case yagoproto.MessageProcessPost:
		accepted, err := e.receive(ctx, req)
		if err != nil {
			return yagoproto.MessageResponse{}, err
		}
		if accepted {
			resp.Response = yagoproto.MessageResponseAccepted
		}
	}

	return resp, nil
}

func (e endpoint) receive(ctx context.Context, req yagoproto.MessageRequest) (bool, error) {
	seed, ok := req.MySeed.Get()
	if !ok || seed.Hash == "" {
		return false, nil
	}

	subject := strings.TrimSpace(req.Subject)
	body := strings.TrimSpace(req.Body)
	if subject == "" || body == "" {
		return false, nil
	}

	message := Message{
		FromName: senderName(seed),
		FromHash: seed.Hash,
		ToName:   e.identity.Name,
		ToHash:   e.identity.Hash,
		Subject:  subject,
		Body:     body,
	}
	if err := wrapReceiveMessage(e.inbox.Receive(ctx, message)); err != nil {
		return false, err
	}

	slog.DebugContext(ctx, "peer message accepted", slog.String("sender", seed.Hash.String()))

	return true, nil
}

func rejectedResponse() yagoproto.MessageResponse {
	return yagoproto.MessageResponse{Response: yagoproto.MessageResponseRejected}
}

type rejectingInbox struct{}

func (rejectingInbox) Receive(context.Context, Message) error {
	return fmt.Errorf("peer message inbox unavailable")
}

func noInbox(inbox Inbox) Inbox {
	if inbox == nil {
		return rejectingInbox{}
	}

	return inbox
}
