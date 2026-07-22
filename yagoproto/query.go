package yagoproto

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	QueryResponseRejected   = -1
	QueryResponseUnresolved = "-UNRESOLVED_PATTERN-"
)

type QueryRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	YouAre             string
	Iam                string
	Object             QueryObject
	Env                string
	Key                string
	MagicMD5           string
	MyTime             string
}

type QueryResponse struct {
	ResponseHeader
	Response           int
	MyTime             string
	Magic              string
	UnresolvedResponse bool
}

func (r QueryRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldYouAre, r.YouAre)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldObject, string(r.Object))
	putString(form, FieldEnv, r.Env)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)

	return form
}

func ParseQueryRequest(_ context.Context, form url.Values) (QueryRequest, error) {
	networkName, networkNamePresent := parseNetworkName(form)
	req := QueryRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		YouAre:             form.Get(FieldYouAre),
		Iam:                form.Get(FieldIam),
		Env:                form.Get(FieldEnv),
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
		MyTime:             form.Get(FieldMyTime),
	}

	req.Object = parseQueryObject(form.Get(FieldObject))

	return req, nil
}

func (r QueryResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	if r.UnresolvedResponse {
		setString(msg, FieldResponse, QueryResponseUnresolved)
	} else {
		setInt(msg, FieldResponse, r.Response)
	}
	myTime := r.MyTime
	if myTime == "" {
		myTime = QueryResponseUnresolved
	}
	setString(msg, FieldMyTime, myTime)
	setString(msg, FieldMagic, r.Magic)

	return msg
}

func ParseQueryResponse(m yagomodel.Message) (QueryResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return QueryResponse{}, err
	}

	if m[FieldResponse] == QueryResponseUnresolved {
		return QueryResponse{
			ResponseHeader:     header,
			Response:           QueryResponseRejected,
			MyTime:             m[FieldMyTime],
			Magic:              m[FieldMagic],
			UnresolvedResponse: true,
		}, nil
	}

	response, err := readInt(FieldResponse, m[FieldResponse])
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
