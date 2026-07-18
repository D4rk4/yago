package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
)

type searchExplanationObservationSink struct{}

func (searchExplanationObservationSink) Observe(
	context.Context,
	[]peerreputation.Observation,
) {
}

func TestDiagnosticRemoteSearchDoesNotWritePeerReputation(t *testing.T) {
	sink := searchExplanationObservationSink{}
	assembly := publicSearchAssembly{peerObservations: sink}
	serving := publicRemoteSearchConfig(assembly)
	diagnostic := publicDiagnosticRemoteSearchConfig(assembly)
	if serving.ReputationObservations == nil {
		t.Fatal("serving remote search lost reputation observations")
	}
	if diagnostic.ReputationObservations != nil {
		t.Fatal("diagnostic remote search retained reputation observations")
	}
}
