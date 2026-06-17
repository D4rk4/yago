package yacyproto

import (
	"fmt"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// ResultErrorNotGranted is the transferURL FieldResult value reporting a refused
// transfer, beyond the shared ResultOK and ResultWrongTarget.
const ResultErrorNotGranted = "error_not_granted"

// TransferURLRequest is the POST /yacy/transferURL.html request: URL metadata
// rows the receiver asked for via a prior transferRWI unknownURL list.
type TransferURLRequest struct {
	NetworkName string
	Iam         yacymodel.Hash
	YouAre      yacymodel.Hash
	URLCount    int
	URLs        []yacymodel.URIMetadataRow
}

// TransferURLResponse is the /yacy/transferURL.html response. Double counts the
// rows the receiver already knew.
type TransferURLResponse struct {
	ResponseHeader
	Result string
	Double int
}

// Form renders the request as HTTP form fields.
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

// ParseTransferURLRequest reads a TransferURLRequest from HTTP form fields.
func ParseTransferURLRequest(form url.Values) (TransferURLRequest, error) {
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

	for i := 0; ; i++ {
		raw := form.Get(indexedKey(prefixURL, i))
		if raw == "" {
			break
		}

		row, err := yacymodel.ParseURIMetadataRow(raw)
		if err != nil {
			return TransferURLRequest{}, fmt.Errorf(
				"transferURL request %s: %w", indexedKey(prefixURL, i), err,
			)
		}

		req.URLs = append(req.URLs, row)
	}

	return req, nil
}

// Encode renders the response as a key=value message.
func (r TransferURLResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	r.write(msg)
	setString(msg, FieldResult, r.Result)
	setInt(msg, FieldDouble, r.Double)

	return msg
}

// ParseTransferURLResponse reads a TransferURLResponse from key=value lines.
func ParseTransferURLResponse(m yacymodel.Message) (TransferURLResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return TransferURLResponse{}, err
	}

	double, err := optionalInt(FieldDouble, m[FieldDouble])
	if err != nil {
		return TransferURLResponse{}, err
	}

	return TransferURLResponse{
		ResponseHeader: header,
		Result:         m[FieldResult],
		Double:         double,
	}, nil
}
