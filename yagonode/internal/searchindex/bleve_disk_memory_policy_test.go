package searchindex

import "testing"

func TestBleveDiskScorchConfigurationBoundsMergeMemory(t *testing.T) {
	configuration := bleveDiskScorchConfiguration()
	persister, ok := configuration["scorchPersisterOptions"].(map[string]interface{})
	if !ok {
		t.Fatalf("persister options = %#v", configuration["scorchPersisterOptions"])
	}
	if persister["MaxSizeInMemoryMergePerWorker"] != diskInMemoryMergeBytes {
		t.Fatalf("in-memory merge bytes = %#v", persister["MaxSizeInMemoryMergePerWorker"])
	}
	if persister["NumPersisterWorkers"] != diskPersisterWorkers {
		t.Fatalf("persister workers = %#v", persister["NumPersisterWorkers"])
	}
	merge, ok := configuration["scorchMergePlanOptions"].(map[string]interface{})
	if !ok {
		t.Fatalf("merge options = %#v", configuration["scorchMergePlanOptions"])
	}
	if merge["MaxSegmentSize"] != diskMaxSegmentDocs {
		t.Fatalf("max segment documents = %#v", merge["MaxSegmentSize"])
	}
	if merge["ReclaimDeletesWeight"] != diskReclaimDeletesWeight {
		t.Fatalf("reclaim weight = %#v", merge["ReclaimDeletesWeight"])
	}
	if configuration["numSnapshotsToKeep"] != diskSnapshotsKept {
		t.Fatalf("snapshots kept = %#v", configuration["numSnapshotsToKeep"])
	}
}
