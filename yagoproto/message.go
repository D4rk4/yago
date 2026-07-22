package yagoproto

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

type MessageProcess string

const (
	MessageProcessPermission MessageProcess = "permission"
	MessageProcessPost       MessageProcess = "post"
)

const (
	MessageResponseRejected   = "-1"
	MessageResponsePermission = "Welcome to my peer!"
	MessageResponseAccepted   = "Thank you!"
)

const (
	MessageSubjectMaximumBytes    = 100
	MessageBodyMaximumBytes       = 10240
	MessageAttachmentMaximumBytes = 0
)

type MessageRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	YouAre             yagomodel.Hash
	Iam                yagomodel.Hash
	Key                string
	MagicMD5           string
	MyTime             string
	Process            MessageProcess
	MySeed             yagomodel.Optional[yagomodel.Seed]
	Subject            string
	Body               string
}

type MessageResponse struct {
	ResponseHeader
	MessageSize    int
	AttachmentSize int
	Response       string
}

func (r MessageRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldYouAre, r.YouAre.String())
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)
	putString(form, FieldMessageProcess, string(r.Process))
	if seed, ok := r.MySeed.Get(); ok {
		putString(form, FieldMySeed, yagomodel.EncodeCompactWireForm(seed.String()))
	}
	putString(form, FieldMessageSubject, r.Subject)
	putString(form, FieldMessage, r.Body)

	return form
}

func ParseMessageRequest(ctx context.Context, form url.Values) (MessageRequest, error) {
	networkName, networkNamePresent := parseNetworkName(form)
	req := MessageRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
		MyTime:             form.Get(FieldMyTime),
		Process:            MessageProcess(form.Get(FieldMessageProcess)),
	}
	if req.Process == "" {
		req.Process = MessageProcessPermission
	}

	var err error
	req.YouAre, err = parseHashField("message request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return MessageRequest{}, err
	}

	if raw := form.Get(FieldIam); raw != "" {
		if iam, err := yagomodel.ParseHash(raw); err == nil {
			req.Iam = iam
		}
	}

	if req.Process == MessageProcessPost {
		if raw := form.Get(FieldMySeed); raw != "" {
			seed, err := decodeSeed(ctx, raw)
			if err != nil {
				return MessageRequest{}, fmt.Errorf("message request %s: %w", FieldMySeed, err)
			}
			req.MySeed = yagomodel.Some(seed)
		}

		req.Subject, err = decodeMessageField(
			ctx,
			FieldMessageSubject,
			form.Get(FieldMessageSubject),
			MessageSubjectMaximumBytes,
		)
		if err != nil {
			return MessageRequest{}, err
		}

		req.Body, err = decodeMessageField(
			ctx, FieldMessage, form.Get(FieldMessage), MessageBodyMaximumBytes,
		)
		if err != nil {
			return MessageRequest{}, err
		}
	}

	return req, nil
}

func (r MessageResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	setInt(msg, FieldMessageSize, r.MessageSize)
	setInt(msg, FieldMessageAttachmentSize, r.AttachmentSize)
	setString(msg, FieldResponse, r.Response)

	return msg
}

func ParseMessageResponse(m yagomodel.Message) (MessageResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return MessageResponse{}, err
	}

	messageSize, err := optionalInt(FieldMessageSize, m[FieldMessageSize])
	if err != nil {
		return MessageResponse{}, err
	}

	attachmentSize, err := optionalInt(FieldMessageAttachmentSize, m[FieldMessageAttachmentSize])
	if err != nil {
		return MessageResponse{}, err
	}

	return MessageResponse{
		ResponseHeader: header,
		MessageSize:    messageSize,
		AttachmentSize: attachmentSize,
		Response:       m[FieldResponse],
	}, nil
}

func decodeMessageField(ctx context.Context, field, raw string, limit int) (string, error) {
	plain, err := yagomodel.DecodeWireFormWithLimit(ctx, raw, int64(limit))
	if err != nil {
		return "", fmt.Errorf("message request %s: %w", field, err)
	}

	return plain, nil
}
