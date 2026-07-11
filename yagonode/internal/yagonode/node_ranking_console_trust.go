package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
)

func (console *rankingConsole) HostTrust(context.Context) adminui.HostTrustView {
	if console.trust == nil {
		return adminui.HostTrustView{Domains: []string{}}
	}
	policy := console.trust.Current()

	return adminui.HostTrustView{
		Blend:   policy.Blend,
		Domains: append([]string(nil), policy.Domains...),
	}
}

func (console *rankingConsole) ApplyHostTrust(
	ctx context.Context,
	view adminui.HostTrustView,
) error {
	if console.trust == nil {
		return fmt.Errorf("host trust catalog is unavailable")
	}
	if err := console.trust.Replace(ctx, hosttrust.Policy{
		Blend: view.Blend, Domains: append([]string(nil), view.Domains...),
	}); err != nil {
		return fmt.Errorf("replace host trust policy: %w", err)
	}

	return nil
}
