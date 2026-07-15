package hostlinkgraph

import "encoding/json"

const HostReferenceRowDefinition = "String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}"

type Graph struct {
	RowDefinition string
	LinkedHosts   []LinkedHost
}

type LinkedHost struct {
	HostHash   string
	References []json.RawMessage
}

func Clone(graph Graph) Graph {
	cloned := Graph{
		RowDefinition: graph.RowDefinition,
		LinkedHosts:   make([]LinkedHost, len(graph.LinkedHosts)),
	}
	for hostIndex, linkedHost := range graph.LinkedHosts {
		references := make([]json.RawMessage, len(linkedHost.References))
		for referenceIndex, reference := range linkedHost.References {
			references[referenceIndex] = append(json.RawMessage(nil), reference...)
		}
		cloned.LinkedHosts[hostIndex] = LinkedHost{
			HostHash:   linkedHost.HostHash,
			References: references,
		}
	}

	return cloned
}
