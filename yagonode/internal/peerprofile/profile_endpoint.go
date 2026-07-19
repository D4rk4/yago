package peerprofile

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	profileContentType = "text/plain; charset=UTF-8"
	profileLineBreak   = "\r\n"
)

type endpoint struct {
	identity nodeidentity.Identity
	profile  Properties
}

func (e endpoint) Serve(
	ctx context.Context,
	req yagoproto.ProfileRequest,
) (httpguard.RawResponse, error) {
	resp := httpguard.RawResponse{ContentType: profileContentType}
	if !e.identity.Authenticates(req.NetworkName, req.Key, req.Iam, req.MagicMD5) {
		return resp, nil
	}

	resp.Body = encodeProperties(e.profile.Properties(ctx))

	return resp, nil
}

func encodeProperties(properties []Property) string {
	size, ok := profileResponseSize(properties)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.Grow(size)
	for _, property := range properties {
		if sanitizedProfilePartSize(property.Key, size) == 0 ||
			sanitizedProfilePartSize(property.Value, size) == 0 {
			continue
		}
		writeSanitizedProfilePart(&b, property.Key)
		b.WriteByte('=')
		writeSanitizedProfilePart(&b, property.Value)
		b.WriteString(profileLineBreak)
	}

	return b.String()
}
