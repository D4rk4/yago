package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

func (fakeRoster) ObserveCaller(context.Context, yagomodel.Seed, yagomodel.PeerType) {}

func (fakeRoster) ObserveResponder(context.Context, yagomodel.Seed) {}

func (reachableRoster) ObserveCaller(context.Context, yagomodel.Seed, yagomodel.PeerType) {}

func (reachableRoster) ObserveResponder(context.Context, yagomodel.Seed) {}
