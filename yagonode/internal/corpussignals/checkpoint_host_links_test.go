package corpussignals

import (
	"encoding/json"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
)

func TestCheckpointCodecAcceptsLegacySnapshotWithoutHostLinks(t *testing.T) {
	record := checkpointRecord{Format: checkpointFormat, Checkpoint: validCheckpoint()}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatal(err)
	}
	checkpoint := legacy["checkpoint"].(map[string]any)
	delete(checkpoint, "host_links")
	delete(checkpoint, "host_links_ready")
	encoded, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := (checkpointCodec{}).Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Checkpoint.HostLinksReady || decoded.Checkpoint.HostLinks.RowDefinition != "" ||
		len(decoded.Checkpoint.HostLinks.LinkedHosts) != 0 {
		t.Fatalf("legacy host links = %#v", decoded.Checkpoint)
	}
}

func TestCheckpointCodecRejectsInvalidHostLinkState(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Checkpoint)
	}{
		{
			name: "unavailable snapshot",
			mutate: func(checkpoint *Checkpoint) {
				checkpoint.HostLinksReady = false
			},
		},
		{
			name: "invalid snapshot",
			mutate: func(checkpoint *Checkpoint) {
				checkpoint.HostLinks.RowDefinition = "invalid"
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := validCheckpoint()
			testCase.mutate(&checkpoint)
			_, err := (checkpointCodec{}).Encode(checkpointRecord{
				Format: checkpointFormat, Checkpoint: checkpoint,
			})
			if err == nil {
				t.Fatalf("invalid host links encoded: %#v", checkpoint.HostLinks)
			}
		})
	}
}

func TestCheckpointCloneOwnsHostLinkSnapshot(t *testing.T) {
	checkpoint := validCheckpoint()
	cloned := cloneCheckpoint(checkpoint)

	checkpoint.HostLinks.LinkedHosts[0].HostHash = "change"
	checkpoint.HostLinks.LinkedHosts[0].References[0][2] = 'x'

	if cloned.HostLinks.LinkedHosts[0].HostHash != "target" ||
		!json.Valid(cloned.HostLinks.LinkedHosts[0].References[0]) ||
		cloned.HostLinks.RowDefinition != hostlinkgraph.HostReferenceRowDefinition {
		t.Fatalf("cloned host links = %#v", cloned.HostLinks)
	}
}
