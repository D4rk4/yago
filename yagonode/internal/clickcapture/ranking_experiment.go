package clickcapture

const (
	AttributionFixed     = "fixed"
	AttributionOriginal  = "original"
	AttributionSwapped   = "swapped"
	AttributionPrimary   = "primary"
	AttributionSecondary = "secondary"
)

type Candidate struct {
	URLIdentity     string
	ClusterIdentity string
	Position        int
}

type DisplayedCandidate struct {
	Candidate
	OriginalIndex int
	Propensity    float64
	Attribution   string
}

func AdjacentPairRandomization(candidates []Candidate, seed uint64) []DisplayedCandidate {
	displayed := make([]DisplayedCandidate, len(candidates))
	for index, candidate := range candidates {
		displayed[index] = DisplayedCandidate{
			Candidate:     candidate,
			OriginalIndex: index,
			Attribution:   AttributionFixed,
		}
	}
	pairOffset := 0
	if experimentDecision(seed, 0) {
		pairOffset = 1
	}
	decision := uint64(1)
	for left := pairOffset; left+1 < len(displayed); left += 2 {
		right := left + 1
		leftPosition := displayed[left].Position
		rightPosition := displayed[right].Position
		attribution := AttributionOriginal
		if experimentDecision(seed, decision) {
			displayed[left], displayed[right] = displayed[right], displayed[left]
			attribution = AttributionSwapped
		}
		displayed[left].Position = leftPosition
		displayed[right].Position = rightPosition
		displayed[left].Propensity = 0.5
		displayed[right].Propensity = 0.5
		displayed[left].Attribution = attribution
		displayed[right].Attribution = attribution
		decision++
	}

	return displayed
}

func TeamDraftInterleave(
	primary []Candidate,
	secondary []Candidate,
	seed uint64,
	limit int,
) []DisplayedCandidate {
	if limit <= 0 {
		return nil
	}
	interleaved := make([]DisplayedCandidate, 0, min(limit, len(primary)+len(secondary)))
	state := teamDraftState{
		seen:        make(map[string]struct{}, len(primary)+len(secondary)),
		interleaved: &interleaved,
		limit:       limit,
	}
	primaryIndex := 0
	secondaryIndex := 0
	round := uint64(0)
	for len(interleaved) < limit {
		primaryFirst := experimentDecision(seed, round)
		before := len(interleaved)
		if primaryFirst {
			primaryIndex = state.draftCandidate(
				primary,
				primaryIndex,
				AttributionPrimary,
			)
			secondaryIndex = state.draftCandidate(
				secondary,
				secondaryIndex,
				AttributionSecondary,
			)
		} else {
			secondaryIndex = state.draftCandidate(
				secondary,
				secondaryIndex,
				AttributionSecondary,
			)
			primaryIndex = state.draftCandidate(
				primary,
				primaryIndex,
				AttributionPrimary,
			)
		}
		if len(interleaved) == before {
			break
		}
		round++
	}

	return interleaved
}

type teamDraftState struct {
	seen        map[string]struct{}
	interleaved *[]DisplayedCandidate
	limit       int
}

func (s teamDraftState) draftCandidate(
	ranking []Candidate,
	start int,
	attribution string,
) int {
	for start < len(ranking) && len(*s.interleaved) < s.limit {
		candidate := ranking[start]
		originalIndex := start
		start++
		identity := candidateIdentity(candidate)
		if _, exists := s.seen[identity]; exists {
			continue
		}
		s.seen[identity] = struct{}{}
		candidate.Position = len(*s.interleaved) + 1
		*s.interleaved = append(*s.interleaved, DisplayedCandidate{
			Candidate:     candidate,
			OriginalIndex: originalIndex,
			Attribution:   attribution,
		})
		break
	}

	return start
}

func candidateIdentity(candidate Candidate) string {
	if candidate.ClusterIdentity != "" {
		return candidate.ClusterIdentity
	}

	return candidate.URLIdentity
}

func experimentDecision(seed, decision uint64) bool {
	value := seed + 0x9e3779b97f4a7c15*(decision+1)
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	value ^= value >> 31

	return value&(uint64(1)<<63) != 0
}
