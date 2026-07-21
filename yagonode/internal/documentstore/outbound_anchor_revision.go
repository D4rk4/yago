package documentstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strings"
)

const MaximumOutboundAnchorSourcesPerReplacement = 16

func canonicalOutboundAnchorSets(
	sets []OutboundAnchorSet,
) ([]OutboundAnchorSet, error) {
	bySource := make(
		map[string]OutboundAnchorSet,
		min(len(sets), MaximumOutboundAnchorSourcesPerReplacement),
	)
	for _, set := range sets {
		sourceURL := strings.TrimSpace(set.SourceURL)
		if !validOutboundAnchorIdentity(sourceURL) {
			continue
		}
		if _, found := bySource[sourceURL]; !found &&
			len(bySource) == MaximumOutboundAnchorSourcesPerReplacement {
			return nil, fmt.Errorf("outbound anchor source limit exceeded")
		}
		set.SourceURL = sourceURL
		set.Anchors = boundedOutboundAnchorSet(sourceURL, set.Anchors)
		bySource[sourceURL] = set
	}
	sources := make([]string, 0, len(bySource))
	for sourceURL := range bySource {
		sources = append(sources, sourceURL)
	}
	sort.Strings(sources)
	canonical := make([]OutboundAnchorSet, 0, len(sources))
	for _, sourceURL := range sources {
		canonical = append(canonical, bySource[sourceURL])
	}

	return canonical, nil
}

func boundedOutboundAnchorSet(
	sourceURL string,
	anchors []OutboundAnchor,
) []OutboundAnchor {
	bounded := make([]OutboundAnchor, 0, min(len(anchors), maximumOutboundAnchors))
	for _, anchor := range anchors {
		targetURL := strings.TrimSpace(anchor.TargetURL)
		if !validOutboundAnchorIdentity(targetURL) || targetURL == sourceURL {
			continue
		}
		anchor.TargetURL = targetURL
		bounded = append(bounded, anchor)
		if len(bounded) == maximumOutboundAnchors {
			break
		}
	}

	return bounded
}

func desiredOutboundAnchorPublication(
	anchors map[string][]AnchorText,
	targets []string,
) outboundAnchorPublication {
	if len(targets) == 0 {
		return outboundAnchorPublication{}
	}
	identity := sha256.New()
	for _, targetURL := range targets {
		writeOutboundAnchorRevisionValue(identity, targetURL)
		for _, anchor := range anchors[targetURL] {
			writeOutboundAnchorRevisionValue(identity, anchor.URL)
			writeOutboundAnchorRevisionValue(identity, anchor.Text)
			flags := byte(0)
			if anchor.NoFollow {
				flags |= 1
			}
			if anchor.UserGenerated {
				flags |= 2
			}
			if anchor.Sponsored {
				flags |= 4
			}
			_, _ = identity.Write([]byte{flags})
		}
	}

	return outboundAnchorPublication{
		Targets:  append([]string(nil), targets...),
		Revision: hex.EncodeToString(identity.Sum(nil)),
	}
}

func writeOutboundAnchorRevisionValue(identity hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = identity.Write(size[:])
	_, _ = identity.Write([]byte(value))
}
