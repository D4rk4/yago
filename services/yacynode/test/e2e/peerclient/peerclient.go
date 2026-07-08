//go:build e2e

// Package peerclient speaks YaCy's peer wire protocol (query.html object
// counts, hello handshake) against either the real YaCy peer or the
// node-under-test, since both implement the same protocol.
package peerclient

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	// Port is the internal HTTP port every YaCy-protocol peer listens on.
	Port = "8090"
	// ExposedPort is Port in Docker's exposed-port notation.
	ExposedPort = Port + "/tcp"
)

const helloProbeHash = "REMOTEPEER12"

func QueryCount(
	ctx context.Context,
	probe *httpprobe.Probe,
	peerURL string,
	hash yacymodel.Hash,
	object yacyproto.QueryObject,
) (int, bool) {
	queryURL := peerURL + "/yacy/query.html?" + url.Values{
		yacyproto.FieldNetworkName: {yacyproto.DefaultNetwork},
		yacyproto.FieldYouAre:      {hash.String()},
		yacyproto.FieldObject:      {string(object)},
	}.Encode()
	result := probe.Get(ctx, queryURL)
	if !result.OK {
		return 0, false
	}
	return responseCount(result.Body)
}

func responseCount(body string) (int, bool) {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "response="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

func ResolveHash(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	peerURL string,
) yacymodel.Hash {
	t.Helper()
	req := yacyproto.HelloRequest{
		NetworkName: yacyproto.DefaultNetwork,
		Iam:         yacymodel.Hash(helloProbeHash),
		Seed:        helloProbeSeed(),
	}
	result := probe.Get(ctx, peerURL+"/yacy/hello.html?"+req.Form().Encode())
	if !result.OK {
		t.Fatal("hello request failed")
	}
	msg, err := yacymodel.ParseMessage(result.Body)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	resp, err := yacyproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}
	seed, ok := resp.OwnSeed().Get()
	if !ok {
		t.Fatal("own seed not present in hello response")
	}
	return seed.Hash
}

func helloProbeSeed() yacymodel.Seed {
	flags := yacymodel.ZeroFlags()
	flags = flags.Set(yacymodel.FlagDirectConnect, true)
	flags = flags.Set(yacymodel.FlagAcceptRemoteIndex, true)
	port, _ := yacymodel.ParsePort(Port)
	return yacymodel.Seed{
		Hash:     yacymodel.Hash(helloProbeHash),
		Name:     yacymodel.Some("e2e-hello-probe"),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
		Port:     yacymodel.Some(port),
		Version:  yacymodel.Some(yacymodel.YaCyVersion("1.83")),
		UTC:      yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(time.Now())),
		Flags:    yacymodel.Some(flags),
	}
}
