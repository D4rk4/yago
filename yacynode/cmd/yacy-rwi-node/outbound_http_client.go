package main

import (
	"net/http"
	"time"
)

const outboundRequestTimeout = 30 * time.Second

func newOutboundHTTPClient() *http.Client {
	return &http.Client{
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
		Timeout:   outboundRequestTimeout,
	}
}
