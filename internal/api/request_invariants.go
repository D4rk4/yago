package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const multipartContentType = "multipart/form-data"

const requestAcceptedMessage = "request accepted"

type requestGuard struct {
	ident        contracts.Identity
	maxBodyBytes int64
	timeout      time.Duration
}

func (g requestGuard) parse(
	w http.ResponseWriter,
	r *http.Request,
	methods yacyproto.EndpointMethodSet,
) (url.Values, context.Context, context.CancelFunc, bool) {
	if !methodAllowed(r.Method, methods) {
		failMethodNotAllowed(r.Context(), w, r.Method)

		return nil, nil, nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, g.maxBodyBytes)
	if err := decodeRequestBody(r); err != nil {
		failBadRequest(r.Context(), w, err)

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
			failRequestTooLarge(r.Context(), w, err)
		} else {
			failBadRequest(r.Context(), w, err)
		}

		return nil, nil, nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)

	slog.DebugContext(ctx, requestAcceptedMessage,
		"method", r.Method,
		"path", r.URL.Path,
		"form", r.Form,
	)

	return r.Form, ctx, cancel, true
}

func (g requestGuard) networkMatches(form url.Values) bool {
	return yacyproto.NetworkUnit(form.Get(yacyproto.FieldNetworkName)) ==
		yacyproto.NetworkUnit(g.ident.NetworkName())
}

func (g requestGuard) youAreMatches(youare yacymodel.Hash) bool {
	return youare == g.ident.Hash()
}

func methodAllowed(method string, methods yacyproto.EndpointMethodSet) bool {
	switch method {
	case http.MethodGet:
		return methods&yacyproto.EndpointMethodGet != 0
	case http.MethodPost:
		return methods&yacyproto.EndpointMethodPost != 0
	default:
		return false
	}
}

func responseHeader(snapshot contracts.StatusSnapshot) yacyproto.ResponseHeader {
	return yacyproto.ResponseHeader{Version: snapshot.Version, Uptime: snapshot.Uptime}
}
