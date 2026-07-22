package dhtexchange

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type OutboundPostingFinalizer interface {
	FinalizeOutboundPostings(context.Context, []yagomodel.RWIPosting) (int, error)
}
