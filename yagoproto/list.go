package yagoproto

import (
	"context"
	"net/url"
)

const ListColumnBlack = "black"

type ListRequest struct {
	NetworkName string
	Iam         string
	Key         string
	MagicMD5    string
	Column      string
	Name        string
}

func (r ListRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldListColumn, r.Column)
	putString(form, FieldListName, r.Name)

	return form
}

func ParseListRequest(_ context.Context, form url.Values) (ListRequest, error) {
	return ListRequest{
		NetworkName: form.Get(FieldNetworkName),
		Iam:         form.Get(FieldIam),
		Key:         form.Get(FieldKey),
		MagicMD5:    form.Get(FieldMagicMD5),
		Column:      form.Get(FieldListColumn),
		Name:        form.Get(FieldListName),
	}, nil
}
