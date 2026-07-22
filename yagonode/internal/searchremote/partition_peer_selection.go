package searchremote

import "github.com/D4rk4/yago/yagomodel"

type partitionPeerSelection struct {
	targets        [][][]yagomodel.Seed
	maximum        int
	partitionTotal int
	selected       []yagomodel.Seed
	seen           map[yagomodel.Hash]struct{}
}

func newPartitionPeerSelection(
	targets [][][]yagomodel.Seed,
	maximum int,
) *partitionPeerSelection {
	if len(targets) == 0 || maximum <= 0 {
		return nil
	}
	partitionTotal := len(targets[0])

	return &partitionPeerSelection{
		targets:        targets,
		maximum:        maximum,
		partitionTotal: partitionTotal,
		selected: make(
			[]yagomodel.Seed,
			0,
			min(maximum, partitionTotal*len(targets)),
		),
		seen: make(map[yagomodel.Hash]struct{}),
	}
}

func (s *partitionPeerSelection) coverPartitions() bool {
	for termRound := range len(s.targets) {
		for partition := range s.partitionTotal {
			termPosition := (partition + termRound) % len(s.targets)
			partitionTargets := s.targets[termPosition][partition]
			if len(partitionTargets) == 0 ||
				partitionHasSelectedPeer(partitionTargets, s.seen) {
				continue
			}
			if !s.append(partitionTargets[0]) {
				return false
			}
		}
	}

	return true
}

func (s *partitionPeerSelection) appendRemainingCandidates() {
	for candidatePosition := 0; s.appendCandidatePosition(candidatePosition); candidatePosition++ {
	}
}

func (s *partitionPeerSelection) appendCandidatePosition(candidatePosition int) bool {
	foundCandidate := false
	for termRound := range len(s.targets) {
		for partition := range s.partitionTotal {
			termPosition := (partition + termRound) % len(s.targets)
			partitionTargets := s.targets[termPosition][partition]
			if candidatePosition >= len(partitionTargets) {
				continue
			}
			foundCandidate = true
			if !s.append(partitionTargets[candidatePosition]) {
				return false
			}
		}
	}

	return foundCandidate
}

func (s *partitionPeerSelection) append(peer yagomodel.Seed) bool {
	if _, found := s.seen[peer.Hash]; found {
		return true
	}
	if len(s.selected) >= s.maximum {
		return false
	}
	s.seen[peer.Hash] = struct{}{}
	s.selected = append(s.selected, peer)

	return true
}
