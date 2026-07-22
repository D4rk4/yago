package yagoproto

import (
	"context"
	"net/url"
)

const ListColumnBlack = "black"

type ListRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	Iam                string
	Key                string
	MagicMD5           string
	Column             string
	Name               string
}

func (r ListRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldListColumn, r.Column)
	putString(form, FieldListName, r.Name)

	return form
}

func ParseListRequest(_ context.Context, form url.Values) (ListRequest, error) {
	networkName, networkNamePresent := parseNetworkName(form)
	return ListRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		Iam:                form.Get(FieldIam),
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
		Column:             form.Get(FieldListColumn),
		Name:               form.Get(FieldListName),
	}, nil
}
