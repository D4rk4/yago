package yagoproto

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	CrawlReceiptResultUnavailable = "unavailable"
	CrawlReceiptResultException   = "exception"
	CrawlReceiptResultRobot       = "robot"
	CrawlReceiptResultRejected    = "rejected"
	CrawlReceiptResultDequeue     = "dequeue"
	CrawlReceiptResultFill        = "fill"
	CrawlReceiptResultUpdate      = "update"
	CrawlReceiptResultKnown       = "known"
	CrawlReceiptResultStale       = "stale"

	MaximumCrawlReceiptResultBytes   = 32
	MaximumCrawlReceiptReasonBytes   = 1024
	MaximumCrawlReceiptMetadataBytes = 256 << 10
)

type CrawlReceiptRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	Iam                yagomodel.Hash
	YouAre             yagomodel.Hash
	Key                string
	MagicMD5           string
	Result             string
	Reason             string
	LURLEntry          string
}

type CrawlReceiptResponse struct {
	ResponseHeader
	Delay int
}

func (r CrawlReceiptRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldYouAre, r.YouAre.String())
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldResult, r.Result)
	putString(form, FieldReason, r.Reason)
	putString(form, FieldLURLEntry, r.LURLEntry)

	return form
}

func ParseCrawlReceiptRequest(_ context.Context, form url.Values) (CrawlReceiptRequest, error) {
	if len(form.Get(FieldResult)) > MaximumCrawlReceiptResultBytes {
		return CrawlReceiptRequest{}, fmt.Errorf("%s exceeds maximum length", FieldResult)
	}
	if len(form.Get(FieldReason)) > MaximumCrawlReceiptReasonBytes {
		return CrawlReceiptRequest{}, fmt.Errorf("%s exceeds maximum length", FieldReason)
	}
	if len(form.Get(FieldLURLEntry)) > MaximumCrawlReceiptMetadataBytes {
		return CrawlReceiptRequest{}, fmt.Errorf("%s exceeds maximum length", FieldLURLEntry)
	}
	networkName, networkNamePresent := parseNetworkName(form)
	req := CrawlReceiptRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
		Result:             form.Get(FieldResult),
		Reason:             form.Get(FieldReason),
		LURLEntry:          form.Get(FieldLURLEntry),
	}

	if raw := form.Get(FieldIam); raw != "" {
		if iam, err := yagomodel.ParseHash(raw); err == nil {
			req.Iam = iam
		}
	}

	if raw := form.Get(FieldYouAre); raw != "" {
		if youare, err := yagomodel.ParseHash(raw); err == nil {
			req.YouAre = youare
		}
	}

	return req, nil
}

func ValidCrawlReceiptResult(result string) bool {
	switch result {
	case CrawlReceiptResultUnavailable,
		CrawlReceiptResultException,
		CrawlReceiptResultRobot,
		CrawlReceiptResultRejected,
		CrawlReceiptResultDequeue,
		CrawlReceiptResultFill,
		CrawlReceiptResultUpdate,
		CrawlReceiptResultKnown,
		CrawlReceiptResultStale:
		return true
	default:
		return false
	}
}

func (r CrawlReceiptResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	if r.Delay == 0 {
		return msg
	}

	setInt(msg, FieldDelay, r.Delay)

	return msg
}

func ParseCrawlReceiptResponse(m yagomodel.Message) (CrawlReceiptResponse, error) {
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
