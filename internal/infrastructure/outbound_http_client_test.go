package infrastructure

import (
	"net/http"
	"testing"
)

func TestNewOutboundHTTPClient(t *testing.T) {
	client := NewOutboundHTTPClient()
	if client.Timeout != outboundRequestTimeout {
		t.Errorf("timeout = %v", client.Timeout)
	}
	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Errorf("transport = %T, want *http.Transport", client.Transport)
	}
}
