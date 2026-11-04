package documentstore

import "context"

func (d documentVault) clearInboundAnchorRefreshes(ctx context.Context, targetURLs []string) error {
	mutations := make([]outboundAnchorTargetMutation, 0, len(targetURLs))
	for _, targetURL := range targetURLs {
		mutation := outboundAnchorTargetMutation{targetURL: targetURL, clearRefresh: true}
		mutation.encodedBytes = outboundAnchorTargetMutationEncodedBytes(mutation)
		mutations = append(mutations, mutation)
	}

	return d.storeOutboundAnchorTargetMutations(ctx, mutations)
}
