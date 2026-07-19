package yagonode

import "testing"

func TestNodeStorageExposesPostingPagesToQuotaEviction(t *testing.T) {
	vault := openTestVault(t)
	storage, err := openNodeStorage(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	if storage.postingPages == nil {
		t.Fatal("posting page source unavailable")
	}
	if newStorageSweeper(vault, storage) == nil {
		t.Fatal("storage sweeper unavailable")
	}
}
