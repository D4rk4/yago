package yagonode

import "net/http"

func nodePeerClients(
	config nodeConfig,
	client *http.Client,
) (seedlistClient, peerProtocolClient *http.Client) {
	seedlistClient = client
	if config.PeerHTTPSPreferred {
		seedlistClient = newRuntimePeerProtocolClient(config)
	}
	return seedlistClient, peerClientWithoutRedirects(seedlistClient)
}

func peerClientWithoutRedirects(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}
