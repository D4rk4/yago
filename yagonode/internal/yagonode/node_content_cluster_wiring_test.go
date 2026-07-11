package yagonode

import "testing"

func TestOpenNodeStorageCarriesPersistentContentClusters(t *testing.T) {
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	if storage.contentClusters == nil {
		t.Fatal("content clusters are not carried by node storage")
	}
	if _, found, err := storage.contentClusters.Cluster(
		t.Context(),
		"missing",
	); err != nil ||
		found {
		t.Fatalf("fresh content cluster lookup = %v, %v", found, err)
	}
}
