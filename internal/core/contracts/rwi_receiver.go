package contracts

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIReceipt struct {
	UnknownURL []yacymodel.Hash
	ErrorURL   []yacymodel.Hash
	Pause      int
	Busy       bool
}

type RWIReceiver interface {
	ReceiveRWI(ctx context.Context, entries []yacymodel.RWIEntry) (RWIReceipt, error)
}
