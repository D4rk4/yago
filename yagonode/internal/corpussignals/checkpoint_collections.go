package corpussignals

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
)

func validateCheckpointCollections(checkpoint Checkpoint) error {
	if checkpoint.Authority == nil || checkpoint.Citations == nil || checkpoint.Spelling == nil ||
		checkpoint.WordForms == nil || checkpoint.TrustDomains == nil {
		return fmt.Errorf("corpus signal checkpoint contains missing collections")
	}
	if len(checkpoint.Authority) > maximumCheckpointAuthorityDomains ||
		len(checkpoint.Citations) > maximumCheckpointCitations ||
		len(checkpoint.Spelling) > maximumCheckpointSpellingTerms ||
		len(checkpoint.WordForms) > maximumCheckpointWordFormTerms ||
		len(checkpoint.TrustDomains) > maximumCheckpointTrustDomains {
		return fmt.Errorf("corpus signal checkpoint exceeds collection limits")
	}
	if !checkpoint.WordFormsReady && len(checkpoint.WordForms) > 0 {
		return fmt.Errorf("corpus signal checkpoint has unavailable word forms")
	}
	if checkpoint.HostLinksReady {
		if err := hostlinkgraph.ValidateSnapshot(checkpoint.HostLinks); err != nil {
			return fmt.Errorf("invalid host-link checkpoint: %w", err)
		}

		return nil
	}
	if checkpoint.HostLinks.RowDefinition != "" || len(checkpoint.HostLinks.LinkedHosts) > 0 {
		return fmt.Errorf("corpus signal checkpoint has unavailable host links")
	}

	return nil
}
