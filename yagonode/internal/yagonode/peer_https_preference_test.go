package yagonode

import (
	"net/http"
	"testing"
)

func TestLoadNodeConfigReadsPeerHTTPSPreference(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:           "0123456789AB",
		envPeerName:           "node",
		envPeerHTTPSPreferred: "true",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.PeerHTTPSPreferred {
		t.Error("PeerHTTPSPreferred = false, want true when enabled")
	}
}

func TestLoadNodeConfigRejectsInvalidPeerHTTPSPreference(t *testing.T) {
	_, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:           "0123456789AB",
		envPeerName:           "node",
		envPeerHTTPSPreferred: "maybe",
	}))
	if err == nil {
		t.Fatal("load config error = nil, want an error for an unparseable boolean")
	}
}

func TestPeerProtocolClientToleratesSelfSignedCertificates(t *testing.T) {
	client := newRuntimePeerProtocolClient(nodeConfig{})

	branded, ok := client.Transport.(userAgentTransport)
	if !ok {
		t.Fatalf("transport = %T, want userAgentTransport", client.Transport)
	}
	transport, ok := branded.next.(*http.Transport)
	if !ok {
		t.Fatalf("inner transport = %T, want *http.Transport", branded.next)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("peer protocol client should tolerate self-signed peer certificates")
	}
}

func TestAssembledNodeUsesPeerProtocolClientWhenHTTPSPreferred(t *testing.T) {
	config := testConfig(t)
	config.PeerHTTPSPreferred = true

	assembleTestNode(t, config, openTestVault(t))
}

func TestRemoteSearchClientPrefersPeerClient(t *testing.T) {
	base := &http.Client{}
	peer := &http.Client{}
	if remoteSearchClient(publicSearchAssembly{client: base, peerClient: peer}) != peer {
		t.Fatal("wired peer client should win")
	}
	if remoteSearchClient(publicSearchAssembly{client: base}) != base {
		t.Fatal("missing peer client should fall back to the general client")
	}
}
