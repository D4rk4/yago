package robots

// DenialObserver receives robots.txt denials so a caller can record them, for
// example as metrics. Implementations must be safe for concurrent use.
type DenialObserver interface {
	RobotsDenied()
}

type noopDenialObserver struct{}

func (noopDenialObserver) RobotsDenied() {}

type Option func(*RobotsAdmissionFetcher)

func WithDenialObserver(observer DenialObserver) Option {
	return func(f *RobotsAdmissionFetcher) {
		if observer != nil {
			f.observer = observer
		}
	}
}
