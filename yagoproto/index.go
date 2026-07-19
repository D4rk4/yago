package yagoproto

import (
	"context"
	"net/url"
)

type IndexRequest struct {
	NetworkName string
	Iam         string
	Key         string
	MagicMD5    string
	Object      string
}

func (r IndexRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldObject, r.Object)

	return form
}

func ParseIndexRequest(_ context.Context, form url.Values) (IndexRequest, error) {
	return IndexRequest{
		NetworkName: form.Get(FieldNetworkName),
		Iam:         form.Get(FieldIam),
		Key:         form.Get(FieldKey),
		MagicMD5:    form.Get(FieldMagicMD5),
		Object:      form.Get(FieldObject),
	}, nil
}
