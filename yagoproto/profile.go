package yagoproto

import (
	"context"
	"net/url"
)

type ProfileRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	Iam                string
	Key                string
	MagicMD5           string
}

func (r ProfileRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)

	return form
}

func ParseProfileRequest(_ context.Context, form url.Values) (ProfileRequest, error) {
	networkName, networkNamePresent := parseNetworkName(form)
	return ProfileRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		Iam:                form.Get(FieldIam),
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
	}, nil
}
