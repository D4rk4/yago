package contracts

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type URLReceipt struct {
	Double   int
	ErrorURL []yacymodel.Hash
	Busy     bool
}

type URLReceiver interface {
	ReceiveURLs(ctx context.Context, rows []yacymodel.URIMetadataRow) (URLReceipt, error)
}
