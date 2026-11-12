package adminui

import "context"

type FreelistCheckpointResult struct {
	ShardsCheckpointed int
	ShardsTotal        int
}

type FreelistCheckpointer interface {
	CheckpointFreelists(context.Context) (FreelistCheckpointResult, error)
}
