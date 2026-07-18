package firefoxfetch

func selectBrowserSlotAcquisitionDeadlineObserver(observers []func()) func() {
	if len(observers) > 0 && observers[0] != nil {
		return observers[0]
	}

	return func() {}
}
