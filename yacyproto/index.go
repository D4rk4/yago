package yacyproto

import (
	"context"
	"net/url"
)

type IndexRequest struct {
	NetworkName string
	Object      string
}

func (r IndexRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldObject, r.Object)

	return form
}

func ParseIndexRequest(_ context.Context, form url.Values) (IndexRequest, error) {
	return IndexRequest{
		NetworkName: form.Get(FieldNetworkName),
		Object:      form.Get(FieldObject),
	}, nil
}
