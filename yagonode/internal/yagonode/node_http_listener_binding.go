package yagonode

import (
	"errors"
	"fmt"
	"net"
	"net/http"
)

type boundHTTPServer struct {
	namedServer
	listener net.Listener
}

var (
	bindHTTPListener = func(server *http.Server) (net.Listener, error) {
		return net.Listen("tcp", server.Addr)
	}
	serveHTTPListener = func(server *http.Server, listener net.Listener) error {
		return server.Serve(listener)
	}
)

func bindHTTPServers(servers []namedServer) ([]boundHTTPServer, error) {
	bound := make([]boundHTTPServer, 0, len(servers))
	for _, server := range servers {
		listener, err := bindHTTPListener(server.server)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("bind %s: %w", server.name, err),
				closeBoundHTTPServers(bound),
			)
		}
		bound = append(bound, boundHTTPServer{namedServer: server, listener: listener})
	}

	return bound, nil
}

func closeBoundHTTPServers(servers []boundHTTPServer) error {
	failures := make([]error, 0, len(servers))
	for _, server := range servers {
		if err := server.listener.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close %s listener: %w", server.name, err))
		}
	}

	return errors.Join(failures...)
}
