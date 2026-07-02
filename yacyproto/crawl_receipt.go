package yacyproto

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yacymodel"
)

type CrawlReceiptRequest struct {
	NetworkName string
	Iam         yacymodel.Hash
	YouAre      yacymodel.Hash
	Result      string
	Reason      string
	LURLEntry   string
}

type CrawlReceiptResponse struct {
	ResponseHeader
	Delay int
}

func (r CrawlReceiptRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldYouAre, r.YouAre.String())
	putString(form, FieldResult, r.Result)
	putString(form, FieldReason, r.Reason)
	putString(form, FieldLURLEntry, r.LURLEntry)

	return form
}

func ParseCrawlReceiptRequest(_ context.Context, form url.Values) (CrawlReceiptRequest, error) {
	req := CrawlReceiptRequest{
		NetworkName: form.Get(FieldNetworkName),
		Result:      form.Get(FieldResult),
		Reason:      form.Get(FieldReason),
		LURLEntry:   form.Get(FieldLURLEntry),
	}

	if raw := form.Get(FieldIam); raw != "" {
		if iam, err := yacymodel.ParseHash(raw); err == nil {
			req.Iam = iam
		}
	}

	if raw := form.Get(FieldYouAre); raw != "" {
		if youare, err := yacymodel.ParseHash(raw); err == nil {
			req.YouAre = youare
		}
	}

	return req, nil
}

func (r CrawlReceiptResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	if r.Delay == 0 {
		return msg
	}

	setInt(msg, FieldDelay, r.Delay)

	return msg
}

func ParseCrawlReceiptResponse(m yacymodel.Message) (CrawlReceiptResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return CrawlReceiptResponse{}, err
	}

	delay, err := optionalInt(FieldDelay, m[FieldDelay])
	if err != nil {
		return CrawlReceiptResponse{}, err
	}

	return CrawlReceiptResponse{ResponseHeader: header, Delay: delay}, nil
}
