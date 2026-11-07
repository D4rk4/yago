package peerwire

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

var ErrInvalidEndpoint = errors.New("invalid peer endpoint")

type Endpoint struct {
	Scheme string
	Host   string
	Port   string
}

func EndpointAt(target *url.URL) Endpoint {
	return Endpoint{Scheme: target.Scheme, Host: target.Hostname(), Port: target.Port()}
}

func (e Endpoint) Origin() string {
	return e.Scheme + "://" + net.JoinHostPort(e.Host, e.Port)
}

func ParseEndpointOrigin(origin string) (Endpoint, error) {
	parsed, err := url.Parse(origin)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	endpoint := EndpointAt(parsed)
	if endpoint.Scheme != schemeHTTP && endpoint.Scheme != schemeHTTPS {
		return Endpoint{}, fmt.Errorf("%w: scheme %q", ErrInvalidEndpoint, endpoint.Scheme)
	}
	if _, err := yagomodel.ParseHost(endpoint.Host); err != nil {
		return Endpoint{}, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	if _, err := yagomodel.ParsePort(endpoint.Port); err != nil {
		return Endpoint{}, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	if endpoint.Origin() != origin {
		return Endpoint{}, fmt.Errorf("%w: %q is not a bare origin", ErrInvalidEndpoint, origin)
	}

	return endpoint, nil
}
