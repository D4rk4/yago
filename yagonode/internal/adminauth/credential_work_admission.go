package adminauth

import "errors"

const maximumConcurrentCredentialWork = 2

var (
	errCredentialWorkUnavailable = errors.New("credential work capacity exceeded")
	credentialWorkSlots          = make(chan struct{}, maximumConcurrentCredentialWork)
	credentialPasswordHash       = hashPassword
	credentialPasswordVerify     = verifyPassword
)

func acquireCredentialWork() (func(), bool) {
	select {
	case credentialWorkSlots <- struct{}{}:
		return func() { <-credentialWorkSlots }, true
	default:
		return nil, false
	}
}

func hashCredentialPassword(password string) (string, error) {
	release, ok := acquireCredentialWork()
	if !ok {
		return "", errCredentialWorkUnavailable
	}
	defer release()

	return credentialPasswordHash(password)
}

func verifyCredentialPassword(encoded, password string) (bool, error) {
	release, ok := acquireCredentialWork()
	if !ok {
		return false, errCredentialWorkUnavailable
	}
	defer release()

	return credentialPasswordVerify(encoded, password)
}
