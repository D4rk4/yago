package adminui

import "context"

type CrawlOrderClearSource interface {
	ClearPendingOrders(context.Context) (int, error)
}
