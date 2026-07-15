package hostlinks

import (
	"context"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
)

type SnapshotHolder struct {
	current atomic.Pointer[Graph]
}

func NewSnapshotHolder() *SnapshotHolder {
	holder := &SnapshotHolder{}
	holder.Replace(Graph{RowDefinition: HostReferenceRowDefinition})

	return holder
}

func (h *SnapshotHolder) IncomingHostLinks(context.Context) Graph {
	current := h.current.Load()
	if current == nil {
		return Graph{RowDefinition: HostReferenceRowDefinition}
	}

	return CloneGraph(*current)
}

func (h *SnapshotHolder) Replace(graph Graph) {
	snapshot := CloneGraph(graph)
	h.current.Store(&snapshot)
}

func CloneGraph(graph Graph) Graph {
	return hostlinkgraph.Clone(graph)
}
