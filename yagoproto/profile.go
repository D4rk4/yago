package yagoproto

import (
	"context"
	"net/url"
)

type ProfileRequest struct {
	NetworkName string
	Iam         string
	Key         string
	MagicMD5    string
}

func (r ProfileRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)

	return form
}

func ParseProfileRequest(_ context.Context, form url.Values) (ProfileRequest, error) {
	return ProfileRequest{
		NetworkName: form.Get(FieldNetworkName),
		Iam:         form.Get(FieldIam),
		Key:         form.Get(FieldKey),
		MagicMD5:    form.Get(FieldMagicMD5),
	}, nil
}
