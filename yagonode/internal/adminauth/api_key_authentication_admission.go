package adminauth

const maximumConcurrentAPIKeyAuthentications = 32

var apiKeyAuthenticationSlots = make(chan struct{}, maximumConcurrentAPIKeyAuthentications)

func acquireAPIKeyAuthentication() (func(), bool) {
	select {
	case apiKeyAuthenticationSlots <- struct{}{}:
		return func() { <-apiKeyAuthenticationSlots }, true
	default:
		return nil, false
	}
}
