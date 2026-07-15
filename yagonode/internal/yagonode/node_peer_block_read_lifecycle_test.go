package yagonode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
)

type peerBlockReadProbe struct {
	peerBlockStore
	read  func(context.Context) ([]peerblock.Blocked, error)
	calls int
}

func (p *peerBlockReadProbe) Blocked(ctx context.Context) ([]peerblock.Blocked, error) {
	p.calls++

	return p.read(ctx)
}

func capturePeerBlockLifecycleLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &output
}

func canceledPeerBlockContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func expiredPeerBlockContext() context.Context {
	ctx, cancel := context.WithDeadline(
		context.Background(),
		time.Now().Add(-time.Second),
	)
	defer cancel()

	return ctx
}

func assertBlockingRosterSkipsBlocklist(
	t *testing.T,
	ctx context.Context,
	reason string,
) {
	t.Helper()
	output := capturePeerBlockLifecycleLogs(t)
	probe := &peerBlockReadProbe{
		peerBlockStore: newFakePeerBlocks(),
		read: func(context.Context) ([]peerblock.Blocked, error) {
			return nil, errors.New("unexpected blocklist read")
		},
	}
	peers := []yagomodel.Seed{{Hash: yagomodel.Hash("AAAAAAAAAAAA")}}
	roster := newBlockingRoster(reachableRoster{peers: peers}, probe)

	if got := roster.ReachablePeers(ctx); len(got) != 1 || got[0].Hash != peers[0].Hash {
		t.Fatalf("reachable peers = %+v, want fail-open peers", got)
	}
	if probe.calls != 0 {
		t.Fatalf("blocklist reads = %d, want 0", probe.calls)
	}
	logged := output.String()
	for _, want := range []string{
		`"level":"DEBUG"`,
		`"msg":"` + peerBlockFanoutReadSkippedMessage + `"`,
		`"reason":"` + reason + `"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log = %q, want %q", logged, want)
		}
	}
	if strings.Contains(logged, `"level":"WARN"`) {
		t.Fatalf("log = %q, unexpected WARN", logged)
	}
}

func TestBlockingRosterSkipsBlocklistAfterRequestEnds(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func() context.Context
		reason string
	}{
		{
			name:   "canceled",
			ctx:    canceledPeerBlockContext,
			reason: peerBlockFanoutCanceledReason,
		},
		{
			name:   "deadline",
			ctx:    expiredPeerBlockContext,
			reason: peerBlockFanoutDeadlineReason,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertBlockingRosterSkipsBlocklist(t, test.ctx(), test.reason)
		})
	}
}

func TestBlockingRosterKeepsReadFailuresAtWarn(t *testing.T) {
	tests := []struct {
		name string
		read func(context.CancelFunc) func(context.Context) ([]peerblock.Blocked, error)
	}{
		{
			name: "live context",
			read: func(context.CancelFunc) func(context.Context) ([]peerblock.Blocked, error) {
				return func(context.Context) ([]peerblock.Blocked, error) {
					return nil, errors.New("storage read failed")
				}
			},
		},
		{
			name: "canceled during read",
			read: func(cancel context.CancelFunc) func(context.Context) ([]peerblock.Blocked, error) {
				return func(context.Context) ([]peerblock.Blocked, error) {
					cancel()

					return nil, errors.New("storage read failed")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := capturePeerBlockLifecycleLogs(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &peerBlockReadProbe{
				peerBlockStore: newFakePeerBlocks(),
				read:           test.read(cancel),
			}
			roster := newBlockingRoster(reachableRoster{peers: []yagomodel.Seed{{
				Hash: yagomodel.Hash("AAAAAAAAAAAA"),
			}}}, probe)

			if got := roster.ReachablePeers(ctx); len(got) != 1 {
				t.Fatalf("reachable peers = %d, want fail-open peer", len(got))
			}
			if probe.calls != 1 {
				t.Fatalf("blocklist reads = %d, want 1", probe.calls)
			}
			logged := output.String()
			for _, want := range []string{
				`"level":"WARN"`,
				`"msg":"` + peerBlockFanoutReadFailedMessage + `"`,
				`"error":"storage read failed"`,
			} {
				if !strings.Contains(logged, want) {
					t.Fatalf("log = %q, want %q", logged, want)
				}
			}
		})
	}
}
