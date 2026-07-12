package tavilyapi

import (
	"context"
	"io"
	"net/http"
	"time"
)

const (
	maximumConcurrentRawContentWork = 4
	maximumRawContentWorkDuration   = 30 * time.Second
)

var rawContentWorkAdmission = newRequestAdmission(maximumConcurrentRawContentWork)

func enterRawContentWork(w http.ResponseWriter, id string) (func(), bool) {
	release, admitted := rawContentWorkAdmission.tryEnter()
	if admitted {
		return release, true
	}
	w.Header().Set("Retry-After", "1")
	writeError(
		w,
		http.StatusServiceUnavailable,
		"raw_content_capacity_exceeded",
		"raw content capacity exceeded, try again later",
		id,
	)

	return nil, false
}

func rawContentWorkContext(
	parent context.Context,
	duration time.Duration,
) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		duration = maximumRawContentWorkDuration
	}

	return context.WithTimeout(parent, duration)
}

func closeRequestBodyWhenDone(ctx context.Context, body io.Closer) func() bool {
	return context.AfterFunc(ctx, func() { _ = body.Close() })
}

func enterOptionalRawContentWork(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	enabled bool,
	duration time.Duration,
) (*http.Request, func(), bool) {
	if !enabled {
		return r, func() {}, true
	}
	release, admitted := enterRawContentWork(w, id)
	if !admitted {
		return r, nil, false
	}
	ctx, cancel := rawContentWorkContext(r.Context(), duration)

	return r.WithContext(ctx), func() {
		cancel()
		release()
	}, true
}

func (e searchEndpoint) enterWork(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	rawContent bool,
) (*http.Request, func(), bool) {
	r, releaseRawContent, admitted := enterOptionalRawContentWork(
		w,
		r,
		id,
		rawContent,
		e.rawWorkDuration,
	)
	if !admitted {
		return r, nil, false
	}
	if e.admission == nil {
		return r, releaseRawContent, true
	}
	releaseSearch, admitted := enterSearchAdmission(w, r, id, e.admission)
	if !admitted {
		releaseRawContent()

		return r, nil, false
	}

	return r, func() {
		releaseSearch()
		releaseRawContent()
	}, true
}
