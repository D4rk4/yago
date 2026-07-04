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
	if !e.identity.NetworkMatches(req.NetworkName) {
		return resp, nil
	}

	resp.Body = encodeProperties(e.profile.Properties(ctx))

	return resp, nil
}

func encodeProperties(properties []Property) string {
	var b strings.Builder
	for _, property := range properties {
		key := sanitizePropertyPart(property.Key)
		value := sanitizePropertyPart(property.Value)
		if key == "" || value == "" {
			continue
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteString(profileLineBreak)
	}

	return b.String()
}

func sanitizePropertyPart(value string) string {
	value = strings.ReplaceAll(value, "\r", "")

	return strings.ReplaceAll(value, "\n", "\\n")
}
