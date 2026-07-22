package dhtexchange

import "github.com/D4rk4/yago/yagomodel"

type outboundPostingIdentity struct {
	word yagomodel.Hash
	url  yagomodel.Hash
}

func (q *OutboundQueue) confirmablePostings(
	postings []yagomodel.RWIPosting,
) []yagomodel.RWIPosting {
	pending := make(map[outboundPostingIdentity]struct{})
	for _, chunk := range q.chunks {
		for _, posting := range chunk.Postings {
			identity, valid := identifyOutboundPosting(posting)
			if valid {
				pending[identity] = struct{}{}
			}
		}
	}

	confirmable := make([]yagomodel.RWIPosting, 0, len(postings))
	for _, posting := range postings {
		identity, valid := identifyOutboundPosting(posting)
		if !valid {
			continue
		}
		if _, remains := pending[identity]; !remains {
			confirmable = append(confirmable, posting)
		}
	}

	return confirmable
}

func (q *OutboundQueue) cancelPostingCopies(postings []yagomodel.RWIPosting) {
	cancelled := make(map[outboundPostingIdentity]struct{}, len(postings))
	for _, posting := range postings {
		identity, _ := identifyOutboundPosting(posting)
		cancelled[identity] = struct{}{}
	}

	for peer, chunk := range q.chunks {
		retained := chunk.Postings[:0]
		for _, posting := range chunk.Postings {
			identity, _ := identifyOutboundPosting(posting)
			if _, found := cancelled[identity]; !found {
				retained = append(retained, posting)
			}
		}
		if len(retained) == 0 {
			delete(q.chunks, peer)
			continue
		}
		chunk.Postings = retained
	}
}

func splitHandoffPostings(
	postings []yagomodel.RWIPosting,
	reportedRejected []yagomodel.RWIPosting,
) (accepted []yagomodel.RWIPosting, rejected []yagomodel.RWIPosting) {
	rejections := make(map[outboundPostingIdentity]struct{}, len(reportedRejected))
	for _, posting := range reportedRejected {
		identity, valid := identifyOutboundPosting(posting)
		if valid {
			rejections[identity] = struct{}{}
		}
	}

	for _, posting := range postings {
		identity, valid := identifyOutboundPosting(posting)
		if valid {
			if _, found := rejections[identity]; found {
				rejected = append(rejected, posting)
				continue
			}
		}
		accepted = append(accepted, posting)
	}

	return accepted, rejected
}

func identifyOutboundPosting(
	posting yagomodel.RWIPosting,
) (outboundPostingIdentity, bool) {
	word, err := yagomodel.ParseHash(posting.WordHash.String())
	if err != nil {
		return outboundPostingIdentity{}, false
	}
	url, err := posting.URLHash()
	if err != nil {
		return outboundPostingIdentity{}, false
	}

	return outboundPostingIdentity{word: word, url: url.Hash()}, true
}
