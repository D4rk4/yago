package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const multipartContentType = "multipart/form-data"

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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return nil, nil, nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, g.maxBodyBytes)
	if err := decodeRequestBody(r); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

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
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "bad request", http.StatusBadRequest)
		}

		return nil, nil, nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)

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
