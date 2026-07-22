package sharedblacklist

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	listContentType = "text/plain; charset=UTF-8"
)

type endpoint struct {
	identity   nodeidentity.Identity
	blacklists Blacklists
}

func (e endpoint) Serve(
	ctx context.Context,
	req yagoproto.ListRequest,
) (httpguard.RawResponse, error) {
	if !e.identity.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam,
		req.MagicMD5,
	) {
		return httpguard.RawResponse{ContentType: listContentType}, nil
	}

	if req.Column != yagoproto.ListColumnBlack {
		return httpguard.RawResponse{ContentType: listContentType}, nil
	}
	body := e.blacklists.SharedList(ctx, req.Name)
	if len(body) > maximumSharedBlacklistAggregateBytes {
		body = ""
	} else if body != "" {
		body = strings.Clone(body)
	}

	return httpguard.RawResponse{
		ContentType: listContentType,
		Body:        body,
	}, nil
}
