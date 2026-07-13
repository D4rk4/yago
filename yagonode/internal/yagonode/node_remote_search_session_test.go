package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
)

type remoteSessionSequenceSearcher struct {
	responses []searchcore.Response
	calls     int
}

func (s *remoteSessionSequenceSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	response := s.responses[s.calls]
	s.calls++

	return response, nil
}

type emptySessionLocalSearcher struct{}

func (emptySessionLocalSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{}, nil
}

func TestRemoteCapacityRetainsPeerSessionButHonestMissReplacesIt(t *testing.T) {
	admission := make(chan struct{}, 1)
	remote := &remoteSessionSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{
			URL:    "https://peer.example/drunklab",
			Source: searchcore.SourceRemote,
		}}},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Reason: "no known peers",
		}}},
	}}
	retained := remoteSearchRetentionSearcher{inner: remote, admission: admission}
	stable := searchsession.NewStableWindow(searchcore.NewFederatedSearcher(
		emptySessionLocalSearcher{},
		retained,
	))
	req := searchcore.Request{
		Query:  "drunklab",
		Source: searchcore.SourceGlobal,
		Limit:  10,
	}

	first, err := stable.Search(t.Context(), req)
	if err != nil || len(first.Results) != 1 {
		t.Fatalf("first response = %#v, error = %v", first, err)
	}
	admission <- struct{}{}
	busy, err := stable.Search(t.Context(), req)
	if err != nil || len(busy.Results) != 1 ||
		busy.Results[0].URL != "https://peer.example/drunklab" ||
		len(busy.PartialFailures) != 1 ||
		busy.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteStage {
		t.Fatalf("busy response = %#v, error = %v", busy, err)
	}
	<-admission

	honestMiss, err := stable.Search(t.Context(), req)
	if err != nil || len(honestMiss.Results) != 0 ||
		len(honestMiss.PartialFailures) != 1 ||
		honestMiss.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteYaCy {
		t.Fatalf("honest miss = %#v, error = %v", honestMiss, err)
	}
	if recent, ok := stable.Recent(req); ok || len(recent.Results) != 0 {
		t.Fatalf("recent response = %#v, found = %t", recent, ok)
	}
}
