package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	listContentType = "text/plain; charset=UTF-8"
)

type endpoint struct {
	blacklists Blacklists
}

func (e endpoint) Serve(
	ctx context.Context,
	req yacyproto.ListRequest,
) (httpguard.RawResponse, error) {
	if req.Column != yacyproto.ListColumnBlack {
		return httpguard.RawResponse{ContentType: listContentType}, nil
	}

	return httpguard.RawResponse{
		ContentType: listContentType,
		Body:        e.blacklists.SharedList(ctx, req.Name),
	}, nil
}
