// Package rendergate bounds concurrency, deadline, and response size around a renderer.
package rendergate

import (
	"context"
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/renderedpage"
)

const (
	ReasonRenderFailed = "render_failed"
	ReasonTooLarge     = "too_large"
)

type Metrics interface {
	RenderWaited()
	RenderSucceeded()
	RenderFailed(reason string)
	RenderObserved(elapsed time.Duration)
}

type Renderer struct {
	inner    renderedpage.Renderer
	slots    chan struct{}
	deadline time.Duration
	maxBytes int64
	metrics  Metrics
}

func New(
	inner renderedpage.Renderer,
	concurrency int,
	deadline time.Duration,
	maxBytes int64,
	metrics Metrics,
) *Renderer {
	return &Renderer{
		inner:    inner,
		slots:    make(chan struct{}, concurrency),
		deadline: deadline,
		maxBytes: maxBytes,
		metrics:  metrics,
	}
}

func (r *Renderer) Render(ctx context.Context, targetURL string) (renderedpage.Page, error) {
	if err := r.acquire(ctx); err != nil {
		return renderedpage.Page{}, err
	}
	defer func() { <-r.slots }()

	renderCtx, cancel := context.WithTimeout(ctx, r.deadline)
	defer cancel()

	start := time.Now()
	page, err := r.inner.Render(renderCtx, targetURL)
	r.metrics.RenderObserved(time.Since(start))
	if err != nil {
		r.metrics.RenderFailed(ReasonRenderFailed)
		return renderedpage.Page{}, fmt.Errorf("render %s: %w", targetURL, err)
	}
	if int64(len(page.Body)) > r.maxBytes {
		r.metrics.RenderFailed(ReasonTooLarge)
		return renderedpage.Page{}, fmt.Errorf("rendered page exceeds %d bytes", r.maxBytes)
	}

	r.metrics.RenderSucceeded()
	return page, nil
}

func (r *Renderer) acquire(ctx context.Context) error {
	select {
	case r.slots <- struct{}{}:
		return nil
	default:
	}

	r.metrics.RenderWaited()
	select {
	case r.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for render slot: %w", ctx.Err())
	}
}
