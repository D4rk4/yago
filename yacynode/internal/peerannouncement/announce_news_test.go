package peerannouncement

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

type recordingPeerNews struct {
	rotations int
	accepted  []string
}

func (n *recordingPeerNews) RotateSeedNews(context.Context) {
	n.rotations++
}

func (n *recordingPeerNews) AcceptNewsAttachment(_ context.Context, encoded string) {
	n.accepted = append(n.accepted, encoded)
}

func TestAnnounceRotatesNewsAndAcceptsGreetedAttachments(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")
	carrier := callerSeed(t, "known", "198.51.100.7")
	carrier.News = yacymodel.Some("b|encoded-news")
	silent := callerSeed(t, "quiet", "198.51.100.8")

	news := &recordingPeerNews{}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: &stubRoster{rounds: [][]yacymodel.Seed{{peer}}},
		greeter: &stubGreeter{result: greetResult{
			YourType: yacymodel.PeerSenior,
			Known:    []yacymodel.Seed{carrier, silent},
		}},
		news: news,
	}

	a.Announce(ctx)

	if news.rotations != 1 {
		t.Fatalf("rotations = %d, want 1 per announce cycle", news.rotations)
	}
	if len(news.accepted) != 1 || news.accepted[0] != "b|encoded-news" {
		t.Fatalf("accepted = %v, want the carried attachment only", news.accepted)
	}
}

func TestAnnounceWithoutNewsPortSkipsRotation(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")

	a := &announcer{
		self:    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:   &stubSeedSource{},
		roster:  &stubRoster{rounds: [][]yacymodel.Seed{{peer}}},
		greeter: &stubGreeter{result: greetResult{YourType: yacymodel.PeerSenior}},
	}

	a.Announce(ctx)
}
