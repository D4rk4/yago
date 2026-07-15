package adminui

import "context"

type PublicSearchStatus struct {
	Enabled bool
	BaseURL string
}

type PublicSearchStatusSource interface {
	PublicSearchStatus(context.Context) PublicSearchStatus
}
