package adminauth

// AuthObserver receives admin authentication outcomes so a caller can record
// them, for example as metrics. Implementations must be safe for concurrent use.
type AuthObserver interface {
	LoginSucceeded()
	LoginFailed()
	LoginThrottled()
	APIKeyRejected()
	APIKeyThrottled()
	APIKeyForbidden()
}

type noopAuthObserver struct{}

func (noopAuthObserver) LoginSucceeded()  {}
func (noopAuthObserver) LoginFailed()     {}
func (noopAuthObserver) LoginThrottled()  {}
func (noopAuthObserver) APIKeyRejected()  {}
func (noopAuthObserver) APIKeyThrottled() {}
func (noopAuthObserver) APIKeyForbidden() {}
