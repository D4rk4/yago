package adminauth

import "errors"

const maximumAPIKeys = 256

const keyCapacityOperatorMessage = "API key limit reached; revoke an existing key"

var errAPIKeyCapacityReached = errors.New("api key limit reached; revoke an existing key")

func APIKeyCapacityOperatorMessage(err error) (string, bool) {
	if !errors.Is(err, errAPIKeyCapacityReached) {
		return "", false
	}

	return keyCapacityOperatorMessage, true
}
