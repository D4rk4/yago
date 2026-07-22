package peeradmission

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type helloRequestEnvelope struct {
	request yagoproto.HelloRequest
	valid   bool
}

func parseHelloRequestEnvelope(
	ctx context.Context,
	form url.Values,
) (helloRequestEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return helloRequestEnvelope{}, fmt.Errorf("parse hello request: %w", err)
	}
	request, valid := parsedHelloRequest(ctx, form)
	if !valid {
		return helloRequestEnvelope{}, nil
	}

	return helloRequestEnvelope{request: request, valid: true}, nil
}

func parsedHelloRequest(
	ctx context.Context,
	form url.Values,
) (yagoproto.HelloRequest, bool) {
	request, err := yagoproto.ParseHelloRequest(ctx, form)

	return request, err == nil
}

func (e helloEndpoint) ServeEnvelope(
	ctx context.Context,
	envelope helloRequestEnvelope,
) (yagoproto.HelloResponse, error) {
	if !envelope.valid {
		return yagoproto.HelloResponse{
			YourIP:   httpguard.RemoteAddr(ctx),
			YourType: yagomodel.PeerVirgin,
		}, nil
	}

	return e.Serve(ctx, envelope.request)
}
