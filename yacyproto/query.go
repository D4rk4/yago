package yacyproto

import (
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// Known FieldObject values of the query endpoint.
const (
	ObjectRWICount    = "rwicount"
	ObjectRWIURLCount = "rwiurlcount"
	ObjectLURLCount   = "lurlcount"
	ObjectWantedLURLs = "wantedlurls"
	ObjectWantedPURLs = "wantedpurls"
	ObjectWantedWord  = "wantedword"
	ObjectWantedRWI   = "wantedrwi"
	ObjectWantedSeeds = "wantedseeds"
)

// QueryRequest is the GET|POST /yacy/query.html request: a status and capacity
// query. Env carries an object-specific argument, such as a word hash.
type QueryRequest struct {
	NetworkName string
	YouAre      yacymodel.Hash
	Iam         yacymodel.Hash
	Object      string
	Env         string
}

// QueryResponse is the /yacy/query.html response. Response is the numeric
// answer; -1 means rejected or wrong target.
type QueryResponse struct {
	ResponseHeader
	Response int
	MyTime   string
	Magic    string
}

// Form renders the request as HTTP form fields.
func (r QueryRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldYouAre, r.YouAre.String())
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldObject, r.Object)
	putString(form, FieldEnv, r.Env)

	return form
}

// ParseQueryRequest reads a QueryRequest from HTTP form fields.
func ParseQueryRequest(form url.Values) (QueryRequest, error) {
	req := QueryRequest{
		NetworkName: form.Get(FieldNetworkName),
		Object:      form.Get(FieldObject),
		Env:         form.Get(FieldEnv),
	}

	var err error

	req.YouAre, err = parseHashField("query request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return QueryRequest{}, err
	}

	req.Iam, err = parseHashField("query request", FieldIam, form.Get(FieldIam))
	if err != nil {
		return QueryRequest{}, err
	}

	return req, nil
}

// Encode renders the response as a key=value message.
func (r QueryResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	r.write(msg)
	setInt(msg, FieldResponse, r.Response)
	setString(msg, FieldMyTime, r.MyTime)
	setString(msg, FieldMagic, r.Magic)

	return msg
}

// ParseQueryResponse reads a QueryResponse from key=value lines.
func ParseQueryResponse(m yacymodel.Message) (QueryResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return QueryResponse{}, err
	}

	response, err := optionalInt(FieldResponse, m[FieldResponse])
	if err != nil {
		return QueryResponse{}, err
	}

	return QueryResponse{
		ResponseHeader: header,
		Response:       response,
		MyTime:         m[FieldMyTime],
		Magic:          m[FieldMagic],
	}, nil
}
