package crawldispatch

import "github.com/D4rk4/yago/yagocrawlcontract"

type DispatcherOption func(*Dispatcher)

func WithMaxPagesPerRun(source func() int) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		if source != nil {
			dispatcher.maxPagesPerRun = source
		}
	}
}

func (d *Dispatcher) MaxPagesPerRun() int {
	value := yagocrawlcontract.DefaultMaxPagesPerRun
	if d != nil && d.maxPagesPerRun != nil {
		value = d.maxPagesPerRun()
	}
	if value < 0 {
		return yagocrawlcontract.DefaultMaxPagesPerRun
	}

	return value
}
