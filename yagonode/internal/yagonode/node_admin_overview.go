package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

type overviewSource struct {
	report nodestatus.Report
}

func newOverviewSource(report nodestatus.Report) overviewSource {
	return overviewSource{report: report}
}

func (s overviewSource) Overview(ctx context.Context) adminui.Overview {
	seed := s.report.SelfSeed(ctx)

	name, _ := seed.Name.Get()
	peerType, _ := seed.PeerType.Get()
	documents, _ := seed.URLCount.Get()
	words, _ := seed.RWICount.Get()
	knownPeers, _ := seed.KnownSeedCount.Get()
	sentWords, _ := seed.SentWordCount.Get()
	receivedWords, _ := seed.ReceivedWordCount.Get()
	sentURLs, _ := seed.SentURLCount.Get()
	receivedURLs, _ := seed.ReceivedURLCount.Get()

	return adminui.Overview{
		PeerName: name,
		PeerHash: string(seed.Hash),
		PeerType: string(peerType),
		// The branded console reports yago's own build version (buildVersion,
		// stamped by a release build), not the numeric YaCy-compatibility
		// protocol version that report.Version carries for the wire — those two
		// evolve independently and only the latter must stay a YaCy float.
		Version:       Version(),
		UptimeSeconds: s.report.UptimeSeconds(ctx),
		Documents:     documents,
		Words:         words,
		KnownPeers:    knownPeers,
		SentWords:     sentWords,
		ReceivedWords: receivedWords,
		SentURLs:      sentURLs,
		ReceivedURLs:  receivedURLs,
	}
}
