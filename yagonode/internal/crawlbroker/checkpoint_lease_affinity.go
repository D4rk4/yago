package crawlbroker

func leaseRetainsCheckpointAffinity(record leaseRecord) bool {
	return !record.Deferred && record.WorkerID != "" && record.WorkerSessionID != ""
}
