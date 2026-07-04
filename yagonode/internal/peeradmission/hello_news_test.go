package peeradmission

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type recordingNewsIntake struct {
	accepted []string
}

func (n *recordingNewsIntake) AcceptNewsAttachment(_ context.Context, encoded string) {
	n.accepted = append(n.accepted, encoded)
}

func TestHelloAcceptsCallerNewsAttachment(t *testing.T) {
	news := &recordingNewsIntake{}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, &stubReachability{})
	endpoint.news = news

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	caller.News = yagomodel.Some("b|attached-news")

	if _, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", caller, 0),
	); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(news.accepted) != 1 || news.accepted[0] != "b|attached-news" {
		t.Fatalf("accepted = %v, want the caller attachment", news.accepted)
	}
}

func TestHelloIgnoresNewsFromSelfIdentityCaller(t *testing.T) {
	news := &recordingNewsIntake{}
	endpoint := newEndpoint(t, &stubProbe{}, &stubReachability{})
	endpoint.news = news

	self := endpoint.status.SelfSeed(context.Background())
	self.News = yagomodel.Some("b|echoed-news")

	if _, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", self, 0),
	); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(news.accepted) != 0 {
		t.Fatalf("accepted = %v, want none from a self-identity caller", news.accepted)
	}
}

func TestHelloIgnoresForeignNetworkNews(t *testing.T) {
	news := &recordingNewsIntake{}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, &stubReachability{})
	endpoint.news = news

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	caller.News = yagomodel.Some("b|attached-news")

	if _, err := endpoint.Serve(
		context.Background(),
		helloRequest("otherworld", caller, 0),
	); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(news.accepted) != 0 {
		t.Fatalf("accepted = %v, want none across network units", news.accepted)
	}
}
