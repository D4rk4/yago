package infrastructure

import (
	"net/http"
	"time"
)

const outboundRequestTimeout = 30 * time.Second

func NewOutboundHTTPClient() *http.Client {
	return &http.Client{
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
		Timeout:   outboundRequestTimeout,
	}
}
