package frontiercheckpoint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync/atomic"
)

var ErrStateMaximum = errors.New("frontier checkpoint state maximum reached")

type frontierStateGrowthGate struct {
	path         string
	maximumBytes atomic.Uint64
	changed      chan struct{}
}

func newFrontierStateGrowthGate(path string, maximumBytes uint64) *frontierStateGrowthGate {
	gate := &frontierStateGrowthGate{path: path, changed: make(chan struct{}, 1)}
	gate.maximumBytes.Store(maximumBytes)

	return gate
}

func (gate *frontierStateGrowthGate) CheckGrowth() error {
	maximumBytes := gate.maximumBytes.Load()
	if maximumBytes == 0 {
		return nil
	}
	info, err := os.Stat(gate.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect frontier checkpoint state: %w", err)
	}
	if frontierStateMaximumReached(info.Size(), maximumBytes) {
		return ErrStateMaximum
	}

	return nil
}

func frontierStateMaximumReached(stateBytes int64, maximumBytes uint64) bool {
	if maximumBytes > math.MaxInt64 {
		return false
	}

	return stateBytes >= int64(maximumBytes)
}

func (gate *frontierStateGrowthGate) WaitForGrowth(ctx context.Context) (bool, error) {
	for {
		err := gate.CheckGrowth()
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, ErrStateMaximum) {
			return false, err
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-gate.changed:
		}
	}
}

func (gate *frontierStateGrowthGate) SetMaximumBytes(maximumBytes uint64) {
	gate.maximumBytes.Store(maximumBytes)
	select {
	case gate.changed <- struct{}{}:
	default:
	}
}

func (checkpoint *FrontierCheckpoint) CheckGrowth() error {
	return checkpoint.stateGrowth.CheckGrowth()
}

func (checkpoint *FrontierCheckpoint) WaitForGrowth(ctx context.Context) (bool, error) {
	return checkpoint.stateGrowth.WaitForGrowth(ctx)
}

func (checkpoint *FrontierCheckpoint) SetStateMaximumBytes(maximumBytes uint64) {
	checkpoint.stateGrowth.SetMaximumBytes(maximumBytes)
}
