package hostlinkgraph

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCloneOwnsGraphAndReferenceBytes(t *testing.T) {
	want := Graph{
		RowDefinition: HostReferenceRowDefinition,
		LinkedHosts: []LinkedHost{{
			HostHash:   "target",
			References: []json.RawMessage{json.RawMessage(`{"h":"source"}`), nil},
		}},
	}
	input := Clone(want)
	cloned := Clone(input)
	input.RowDefinition = "changed"
	input.LinkedHosts[0].HostHash = "change"
	input.LinkedHosts[0].References[0][2] = 'x'
	input.LinkedHosts[0].References = append(input.LinkedHosts[0].References, json.RawMessage(`{}`))
	input.LinkedHosts = append(input.LinkedHosts, LinkedHost{})

	if !reflect.DeepEqual(cloned, want) {
		t.Fatalf("cloned graph = %#v, want %#v", cloned, want)
	}
}
