package frontier

import "github.com/D4rk4/yago/yago-crawler/internal/crawlpace"

func (f *Frontier) checkpointHostPace(rawURL string) (crawlpace.HostState, int) {
	checkpoint, ok := f.pace.(crawlpace.Checkpoint)
	if !ok {
		return crawlpace.HostState{}, 0
	}

	return checkpoint.SnapshotHost(rawURL), checkpoint.Capacity()
}
