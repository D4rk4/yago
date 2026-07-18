package frontiercheckpoint

import bolt "go.etcd.io/bbolt"

func applyHostProgress(
	transaction *bolt.Tx,
	buckets checkpointBuckets,
	prefix []byte,
	host string,
	progress HostProgress,
) (bool, error) {
	current, err := setHostState(buckets.hosts, prefix, host, progress)
	if err != nil {
		return false, err
	}
	if progress.PaceCapacity > 0 {
		if err := recordHostPace(
			transaction,
			host,
			progress.Pace,
			progress.PaceCapacity,
		); err != nil {
			return false, err
		}
	}

	return current, nil
}
