package searchremote

import "github.com/D4rk4/yago/yagomodel"

type termAbstractCatalog struct {
	terms     map[yagomodel.Hash]map[yagomodel.Hash]struct{}
	peerTerms map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{}
}

func (catalog *termAbstractCatalog) admit(
	term yagomodel.Hash,
	peer yagomodel.Seed,
	resources []yagomodel.Hash,
) {
	identity := peerRankingIdentity(peer)
	if catalog.peerTerms == nil {
		catalog.peerTerms = make(
			map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{},
		)
	}
	if catalog.peerTerms[identity] == nil {
		catalog.peerTerms[identity] = make(
			map[yagomodel.Hash]map[yagomodel.Hash]struct{},
		)
	}
	if catalog.peerTerms[identity][term] == nil {
		catalog.peerTerms[identity][term] = make(map[yagomodel.Hash]struct{})
	}
	for _, resource := range resources {
		catalog.peerTerms[identity][term][resource] = struct{}{}
	}
}

func (catalog termAbstractCatalog) peerAdmitted(
	peer yagomodel.Seed,
	term yagomodel.Hash,
	resource yagomodel.Hash,
) bool {
	_, admitted := catalog.peerTerms[peerRankingIdentity(peer)][term][resource]

	return admitted
}
