package yagoproto

import (
	"context"
	"net/url"
)

type IndexRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	Iam                string
	Key                string
	MagicMD5           string
	Object             string
}

func (r IndexRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldObject, r.Object)

	return form
}

func ParseIndexRequest(_ context.Context, form url.Values) (IndexRequest, error) {
	networkName, networkNamePresent := parseNetworkName(form)
	return IndexRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		Iam:                form.Get(FieldIam),
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
		Object:             form.Get(FieldObject),
	}, nil
}
