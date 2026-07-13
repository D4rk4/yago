package yagonode

import (
	"errors"
	"net/http"
)

func settleListenerExit(listenerError error, servers []namedServer) error {
	shutdownError := shutdown(servers)
	if errors.Is(listenerError, http.ErrServerClosed) {
		return shutdownError
	}

	return errors.Join(listenerError, shutdownError)
}
