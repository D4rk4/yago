package sharedblacklist

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	maximumSharedBlacklistAggregateBytes = 16 << 20
	maximumSharedBlacklistFiles          = 1024
	retainedSharedBlacklistNameBytes     = 64
	maximumConcurrentSharedBlacklist     = 4
)

var errSharedBlacklistBudgetExceeded = errors.New("shared blacklist budget exceeded")

type sharedBlacklistRetention struct {
	remaining int
}

func newSharedBlacklistRetention(maximumBytes int) *sharedBlacklistRetention {
	if maximumBytes <= 0 {
		maximumBytes = maximumSharedBlacklistAggregateBytes
	}

	return &sharedBlacklistRetention{remaining: maximumBytes}
}

func (r *sharedBlacklistRetention) retain(bytes int) error {
	if bytes < 0 || bytes > r.remaining {
		return fmt.Errorf(
			"%w: maximum %d bytes",
			errSharedBlacklistBudgetExceeded,
			maximumSharedBlacklistAggregateBytes,
		)
	}
	r.remaining -= bytes

	return nil
}

type sharedBlacklistReader struct {
	ctx       context.Context
	source    io.Reader
	retention *sharedBlacklistRetention
}

func (r sharedBlacklistReader) Read(destination []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("shared blacklist read: %w", err)
	}
	maximumRead := r.retention.remaining + 1
	if len(destination) > maximumRead {
		destination = destination[:maximumRead]
	}
	read, err := r.source.Read(destination)
	if read > r.retention.remaining {
		r.retention.remaining = 0

		return read, fmt.Errorf("%w: input exceeds retention", errSharedBlacklistBudgetExceeded)
	}
	r.retention.remaining -= read
	if err == nil {
		return read, nil
	}
	if errors.Is(err, io.EOF) {
		return read, io.EOF
	}

	return read, fmt.Errorf("shared blacklist source read: %w", err)
}
