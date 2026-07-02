package sharedblacklist

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	listContentType = "text/plain; charset=UTF-8"
	listLineBreak   = "\r\n"
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
		Body:        encodeEntries(e.blacklists.Entries(ctx, req.Name)),
	}, nil
}

func encodeEntries(entries []string) string {
	if len(entries) == 0 {
		return ""
	}

	return strings.Join(entries, listLineBreak) + listLineBreak
}
