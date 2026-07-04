package yagoproto

import (
	"context"
	"net/url"
)

const ListColumnBlack = "black"

type ListRequest struct {
	NetworkName string
	Column      string
	Name        string
}

func (r ListRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldListColumn, r.Column)
	putString(form, FieldListName, r.Name)

	return form
}

func ParseListRequest(_ context.Context, form url.Values) (ListRequest, error) {
	return ListRequest{
		NetworkName: form.Get(FieldNetworkName),
		Column:      form.Get(FieldListColumn),
		Name:        form.Get(FieldListName),
	}, nil
}
