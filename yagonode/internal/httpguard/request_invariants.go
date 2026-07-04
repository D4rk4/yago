// Package httpguard enforces the HTTP request invariants every module endpoint
// shares: method set, body limit, and request timeout. It carries no feature
// logic and depends only on the wire vocabulary.
package httpguard

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagoproto"
)

const multipartContentType = "multipart/form-data"

const requestAcceptedMessage = "request accepted"

const (
	DefaultMaxBodyBytes   int64 = 4 << 20
	DefaultRequestTimeout       = 30 * time.Second
)

type RequestGuard struct {
	maxBodyBytes int64
	timeout      time.Duration
}

func NewRequestGuard(
	maxBodyBytes int64,
	timeout time.Duration,
) RequestGuard {
	return RequestGuard{maxBodyBytes: maxBodyBytes, timeout: timeout}
}

func (g RequestGuard) Parse(
	w http.ResponseWriter,
	r *http.Request,
	methods yagoproto.EndpointMethodSet,
) (url.Values, context.Context, context.CancelFunc, bool) {
	if !methodAllowed(r.Method, methods) {
		FailMethodNotAllowed(r.Context(), w, r.Method)

		return nil, nil, nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, g.maxBodyBytes)
	if err := decodeRequestBody(r); err != nil {
		FailBadRequest(r.Context(), w, err)

		return nil, nil, nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, g.maxBodyBytes)

	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), multipartContentType) {
		//nolint:gosec // G120: body is bounded by MaxBytesReader above.
		err = r.ParseMultipartForm(g.maxBodyBytes)
	} else {
		err = r.ParseForm()
	}

	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			FailRequestTooLarge(r.Context(), w, err)
		} else {
			FailBadRequest(r.Context(), w, err)
		}

		return nil, nil, nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)

	slog.DebugContext(ctx, requestAcceptedMessage,
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Any("form", r.Form),
	)

	return r.Form, ctx, cancel, true
}

func methodAllowed(method string, methods yagoproto.EndpointMethodSet) bool {
	switch method {
	case http.MethodGet:
		return methods&yagoproto.EndpointMethodGet != 0
	case http.MethodPost:
		return methods&yagoproto.EndpointMethodPost != 0
	default:
		return false
	}
}
