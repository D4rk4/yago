package rwi

import "context"

type StatusSnapshot struct {
	Version string
	Uptime  int
}

type RuntimeStatus interface {
	Snapshot(ctx context.Context) StatusSnapshot
}
