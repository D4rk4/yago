package searchindex

const (
	diskInMemoryMergeBytes = 32 << 20
	diskPersisterWorkers   = 1
)

func bleveDiskScorchConfiguration() map[string]interface{} {
	return map[string]interface{}{
		"scorchMergePlanOptions": map[string]interface{}{
			"MaxSegmentSize":       diskMaxSegmentDocs,
			"ReclaimDeletesWeight": diskReclaimDeletesWeight,
		},
		"scorchPersisterOptions": map[string]interface{}{
			"MaxSizeInMemoryMergePerWorker": diskInMemoryMergeBytes,
			"NumPersisterWorkers":           diskPersisterWorkers,
		},
		"numSnapshotsToKeep": diskSnapshotsKept,
	}
}
