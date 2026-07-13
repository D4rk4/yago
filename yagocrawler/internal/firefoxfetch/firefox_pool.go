package firefoxfetch

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const maximumFirefoxSessions = 2

type firefoxPool struct {
	available chan *firefoxManager
	selection chan struct{}
	managers  []*firefoxManager
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func newFirefoxPool(
	launch BrowserLaunch,
	proxyURL string,
	start func(context.Context, BrowserLaunch, string) (browserSession, error),
) *firefoxPool {
	sessions := launch.Sessions
	if sessions <= 0 {
		sessions = maximumFirefoxSessions
	}
	sessions = min(sessions, maximumFirefoxSessions)
	ctx, cancel := context.WithCancel(context.Background())
	pool := &firefoxPool{
		available: make(chan *firefoxManager, sessions),
		selection: make(chan struct{}, 1),
		managers:  make([]*firefoxManager, 0, sessions),
		ctx:       ctx,
		cancel:    cancel,
	}
	for range sessions {
		manager := &firefoxManager{
			launch:   launch,
			proxyURL: proxyURL,
			timeout:  launch.Timeout,
			start:    start,
		}
		pool.managers = append(pool.managers, manager)
		pool.available <- manager
	}
	pool.selection <- struct{}{}

	return pool
}

func (p *firefoxPool) render(
	ctx context.Context,
	rawURL string,
) (renderedPage, error) {
	if err := p.ctx.Err(); err != nil {
		return renderedPage{}, fmt.Errorf("firefox pool closed: %w", err)
	}
	renderCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	manager, earliest, err := p.acquireRenderable(renderCtx)
	if err != nil {
		return renderedPage{}, err
	}
	if manager != nil {
		page, renderErr := manager.render(renderCtx, rawURL)
		p.release(manager)

		return page, renderErr
	}

	return renderedPage{}, fmt.Errorf(
		"all firefox sessions cooling down until %s",
		earliest.UTC().Format(time.RFC3339),
	)
}

func (p *firefoxPool) acquireRenderable(
	ctx context.Context,
) (*firefoxManager, time.Time, error) {
	select {
	case <-p.selection:
	case <-ctx.Done():
		return nil, time.Time{}, fmt.Errorf("wait for firefox selection: %w", ctx.Err())
	}
	defer func() { p.selection <- struct{}{} }()

	var earliest time.Time
	var cooling []*firefoxManager
	defer func() {
		for _, manager := range cooling {
			p.release(manager)
		}
	}()
	for range len(p.managers) {
		manager, err := p.acquire(ctx)
		if err != nil {
			return nil, time.Time{}, err
		}
		coolingUntil := manager.coolingUntil()
		if !coolingUntil.IsZero() && time.Now().Before(coolingUntil) {
			cooling = append(cooling, manager)
			if earliest.IsZero() || coolingUntil.Before(earliest) {
				earliest = coolingUntil
			}

			continue
		}

		return manager, time.Time{}, nil
	}

	return nil, earliest, nil
}

func (p *firefoxPool) acquire(ctx context.Context) (*firefoxManager, error) {
	select {
	case manager := <-p.available:
		return manager, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for firefox session: %w", ctx.Err())
	}
}

func (p *firefoxPool) release(manager *firefoxManager) {
	select {
	case p.available <- manager:
	case <-p.ctx.Done():
	}
}

func (p *firefoxPool) close() {
	p.closeOnce.Do(func() {
		p.cancel()
		for _, manager := range p.managers {
			manager.close()
		}
	})
}
