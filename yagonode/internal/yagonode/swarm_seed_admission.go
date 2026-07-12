package yagonode

type swarmSeedAdmission struct {
	slots  chan struct{}
	launch func(func())
}

func newSwarmSeedAdmission(capacity int) *swarmSeedAdmission {
	return &swarmSeedAdmission{
		slots:  make(chan struct{}, capacity),
		launch: func(work func()) { go work() },
	}
}

func (a *swarmSeedAdmission) try(work func()) bool {
	select {
	case a.slots <- struct{}{}:
		a.launch(func() {
			defer func() { <-a.slots }()
			work()
		})

		return true
	default:
		return false
	}
}
