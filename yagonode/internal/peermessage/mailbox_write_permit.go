package peermessage

import (
	"context"
	"fmt"
)

type mailboxWritePermit chan struct{}

func newMailboxWritePermit() mailboxWritePermit {
	permit := make(mailboxWritePermit, 1)
	permit <- struct{}{}

	return permit
}

func (p mailboxWritePermit) Acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("acquire mailbox write permit: %w", ctx.Err())
	case <-p:
		return nil
	}
}

func (p mailboxWritePermit) Release() {
	p <- struct{}{}
}
