package yagonode

type remoteCacheWriteAdmission struct {
	slots  chan struct{}
	launch func(func())
}

func newRemoteCacheWriteAdmission(capacity int) *remoteCacheWriteAdmission {
	if capacity < 1 {
		capacity = 1
	}

	return &remoteCacheWriteAdmission{
		slots:  make(chan struct{}, capacity),
		launch: func(work func()) { go work() },
	}
}

func (a *remoteCacheWriteAdmission) try(work func()) bool {
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
