package core

import "context"

type CountKind int

const (
	RWICount CountKind = iota
	RWIURLCount
	LURLCount
)

type Counter interface {
	Count(ctx context.Context, kind CountKind) (int, error)
}
