package yagoproto

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

const QueryResponseRejected = -1

type QueryRequest struct {
	NetworkName string
	YouAre      yagomodel.Hash
	Iam         yagomodel.Hash
	Object      QueryObject
	Env         string
	Key         string
	MagicMD5    string
	MyTime      string
}

type QueryResponse struct {
	ResponseHeader
	Response int
	MyTime   string
	Magic    string
}

func (r QueryRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldYouAre, r.YouAre.String())
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldObject, string(r.Object))
	putString(form, FieldEnv, r.Env)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)

	return form
}

func ParseQueryRequest(_ context.Context, form url.Values) (QueryRequest, error) {
	req := QueryRequest{
		NetworkName: form.Get(FieldNetworkName),
		Env:         form.Get(FieldEnv),
		Key:         form.Get(FieldKey),
		MagicMD5:    form.Get(FieldMagicMD5),
		MyTime:      form.Get(FieldMyTime),
	}

	var err error

	req.Object, err = parseQueryObject(form.Get(FieldObject))
	if err != nil {
		return QueryRequest{}, err
	}

	req.YouAre, err = parseHashField("query request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return QueryRequest{}, err
	}

	if raw := form.Get(FieldIam); raw != "" {
		req.Iam, err = parseHashField("query request", FieldIam, raw)
		if err != nil {
			return QueryRequest{}, err
		}
	}

	return req, nil
}

func (r QueryResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	setInt(msg, FieldResponse, r.Response)
	setString(msg, FieldMyTime, r.MyTime)
	setString(msg, FieldMagic, r.Magic)

	return msg
}

func ParseQueryResponse(m yagomodel.Message) (QueryResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return QueryResponse{}, err
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
