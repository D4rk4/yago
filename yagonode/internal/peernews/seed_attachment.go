package peernews

import (
	"context"
	"log/slog"
	"sync"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	msgNewsRotationFailed   = "news publication rotation failed"
	msgNewsAttachmentBroken = "news attachment rejected"
	msgNewsIntakeFailed     = "news intake failed"
)

type seedAttachment struct {
	mu    sync.RWMutex
	value string
}

func (a *seedAttachment) set(value string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.value = value
}

func (a *seedAttachment) current() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.value
}

func (p *Pool) RotateSeedNews(ctx context.Context) {
	record, ok, err := p.NextPublication(ctx)
	if err != nil {
		slog.WarnContext(ctx, msgNewsRotationFailed, slog.Any("error", err))

		return
	}

	value := ""
	if ok {
		value = yagomodel.EncodeBase64WireForm(record.WireForm())
	}
	p.attachment.set(value)
}

func (p *Pool) SeedNews(context.Context) string {
	return p.attachment.current()
}

func (p *Pool) AcceptNewsAttachment(ctx context.Context, encoded string) {
	if encoded == "" {
		return
	}

	wire, err := yagomodel.DecodeWireForm(ctx, encoded)
	if err != nil {
		slog.WarnContext(ctx, msgNewsAttachmentBroken, slog.Any("error", err))

		return
	}
	record, err := ParseRecord(wire, p.now)
	if err != nil {
		slog.WarnContext(ctx, msgNewsAttachmentBroken, slog.Any("error", err))

		return
	}
	if _, err := p.EnqueueIncomingNews(ctx, record); err != nil {
		slog.WarnContext(ctx, msgNewsIntakeFailed, slog.Any("error", err))
	}
}
