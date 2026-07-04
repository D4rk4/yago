package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	listContentType = "text/plain; charset=UTF-8"
)

type endpoint struct {
	networkName string
	blacklists  Blacklists
}

func (e endpoint) Serve(
	ctx context.Context,
	req yagoproto.ListRequest,
) (httpguard.RawResponse, error) {
	if yagoproto.NetworkUnit(req.NetworkName) != yagoproto.NetworkUnit(e.networkName) {
		return httpguard.RawResponse{ContentType: listContentType}, nil
	}

	if req.Column != yagoproto.ListColumnBlack {
		return httpguard.RawResponse{ContentType: listContentType}, nil
	}

	return httpguard.RawResponse{
		ContentType: listContentType,
		Body:        e.blacklists.SharedList(ctx, req.Name),
	}, nil
}
