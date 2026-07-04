package yagoproto

import (
	"context"
	"net/url"
)

type ProfileRequest struct {
	NetworkName string
}

func (r ProfileRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)

	return form
}

func ParseProfileRequest(_ context.Context, form url.Values) (ProfileRequest, error) {
	return ProfileRequest{NetworkName: form.Get(FieldNetworkName)}, nil
}
