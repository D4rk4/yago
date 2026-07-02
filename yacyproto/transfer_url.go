package yacyproto

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/D4rk4/yago/yacymodel"
)

type TransferURLRequest struct {
	NetworkName string
	Iam         yacymodel.Hash
	YouAre      yacymodel.Hash
	URLCount    int
	URLs        []yacymodel.URIMetadataRow
}

type TransferURLResponse struct {
	ResponseHeader
	Result   TransferURLResult
	Double   int
	ErrorURL []yacymodel.Hash
}

func (r TransferURLRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldYouAre, r.YouAre.String())
	putInt(form, FieldURLCount, r.URLCount)
	for i, row := range r.URLs {
		putString(form, indexedKey(prefixURL, i), row.String())
	}

	return form
}

func ParseTransferURLRequest(ctx context.Context, form url.Values) (TransferURLRequest, error) {
	urlCount, err := optionalInt(FieldURLCount, form.Get(FieldURLCount))
	if err != nil {
		return TransferURLRequest{}, err
	}

	req := TransferURLRequest{
		NetworkName: form.Get(FieldNetworkName),
		URLCount:    urlCount,
	}

	req.Iam, err = parseHashField("transferURL request", FieldIam, form.Get(FieldIam))
	if err != nil {
		return TransferURLRequest{}, err
	}

	req.YouAre, err = parseHashField("transferURL request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return TransferURLRequest{}, err
	}

	for i := 0; i < req.URLCount; i++ {
		raw := form.Get(indexedKey(prefixURL, i))
		if raw == "" {
			slog.WarnContext(
				ctx,
				"transfer url row discarded",
				slog.String("reason", "missing field"),
				slog.Int("index", i),
			)
			continue
		}

		row, err := yacymodel.ParseURIMetadataRow(raw)
		if err != nil {
			slog.WarnContext(
				ctx,
				"transfer url row discarded",
				slog.String("reason", "parse failed"),
				slog.Int("index", i),
				slog.Any("error", err),
			)
			continue
		}

		req.URLs = append(req.URLs, row)
	}

	return req, nil
}

func (r TransferURLResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	if r.Result == "" && r.Double == 0 && len(r.ErrorURL) == 0 {
		return msg
	}

	setString(msg, FieldResult, string(r.Result))
	setInt(msg, FieldDouble, r.Double)
	setString(msg, FieldErrorURL, joinHashes(r.ErrorURL))

	return msg
}

func ParseTransferURLResponse(m yacymodel.Message) (TransferURLResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return TransferURLResponse{}, err
	}

	double, err := optionalInt(FieldDouble, m[FieldDouble])
	if err != nil {
		return TransferURLResponse{}, err
	}

	errorURL, err := splitHashes("transferURL response", FieldErrorURL, m[FieldErrorURL])
	if err != nil {
		return TransferURLResponse{}, err
	}

	result, err := parseTransferURLResult(m[FieldResult])
	if err != nil {
		return TransferURLResponse{}, err
	}

	return TransferURLResponse{
		ResponseHeader: header,
		Result:         result,
		Double:         double,
		ErrorURL:       errorURL,
	}, nil
}
