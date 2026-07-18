package crawlpace

import "time"

type HostState struct {
	NextDueAt       time.Time     `json:"next_due_at,omitempty"`
	BackoffUntil    time.Time     `json:"backoff_until,omitempty"`
	BackoffPenalty  time.Duration `json:"backoff_penalty,omitempty"`
	BackoffFailures uint32        `json:"backoff_failures,omitempty"`
	Generation      uint64        `json:"generation,omitempty"`
}

type Checkpoint interface {
	SnapshotHost(rawURL string) HostState
	RestoreHost(host string, state HostState)
	Capacity() int
}
