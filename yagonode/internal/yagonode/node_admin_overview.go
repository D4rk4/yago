package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

type overviewSource struct {
	report         nodestatus.Report
	localDocuments overviewLocalDocuments
}

func newOverviewSource(report nodestatus.Report) overviewSource {
	return overviewSource{report: report}
}

func (s overviewSource) Overview(ctx context.Context) adminui.Overview {
	seed := s.report.SelfSeed(ctx)

	name, _ := seed.Name.Get()
	peerType, _ := seed.PeerType.Get()
	indexedDocuments, indexedDocumentsKnown := s.localDocuments.read(ctx)
	urlMetadataRecords, urlMetadataRecordsKnown := seedStatistic(seed.URLCount)
	words, wordsKnown := seedStatistic(seed.RWICount)
	knownPeers, knownPeersKnown := seedStatistic(seed.KnownSeedCount)
	sentWords, sentWordsKnown := seedTransferStatistic(seed.SentWordCount)
	receivedWords, receivedWordsKnown := seedTransferStatistic(seed.ReceivedWordCount)
	sentURLs, sentURLsKnown := seedTransferStatistic(seed.SentURLCount)
	receivedURLs, receivedURLsKnown := seedTransferStatistic(seed.ReceivedURLCount)

	return adminui.Overview{
		PeerName: name,
		PeerHash: string(seed.Hash),
		PeerType: string(peerType),
		// The branded console reports yago's own build version (buildVersion,
		// stamped by a release build), not the numeric YaCy-compatibility
		// protocol version that report.Version carries for the wire — those two
		// evolve independently and only the latter must stay a YaCy float.
		Version:                 Version(),
		UptimeSeconds:           s.report.UptimeSeconds(ctx),
		IndexedDocuments:        indexedDocuments,
		IndexedDocumentsKnown:   indexedDocumentsKnown,
		URLMetadataRecords:      urlMetadataRecords,
		URLMetadataRecordsKnown: urlMetadataRecordsKnown,
		Words:                   words,
		WordsKnown:              wordsKnown,
		KnownPeers:              knownPeers,
		KnownPeersKnown:         knownPeersKnown,
		SentWords:               sentWords,
		SentWordsKnown:          sentWordsKnown,
		ReceivedWords:           receivedWords,
		ReceivedWordsKnown:      receivedWordsKnown,
		SentURLs:                sentURLs,
		SentURLsKnown:           sentURLsKnown,
		ReceivedURLs:            receivedURLs,
		ReceivedURLsKnown:       receivedURLsKnown,
	}
}
