package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenSearchLearningStoresSurfacesPeerObserverFailures(t *testing.T) {
	originalOpen := openRuntimePeerReputation
	originalObserver := newRuntimePeerObserver
	defer func() {
		openRuntimePeerReputation = originalOpen
		newRuntimePeerObserver = originalObserver
	}()

	openRuntimePeerReputation = func(
		*vault.Vault,
		peerreputation.Configuration,
	) (*peerreputation.ReputationLedger, error) {
		return nil, errors.New("open")
	}
	if _, err := openSearchLearningStores(context.Background(), learningVault(t)); err == nil {
		t.Fatal("peer reputation open failure did not surface")
	}

	openRuntimePeerReputation = originalOpen
	newRuntimePeerObserver = func(
		context.Context,
		peerReputationBatchLedger,
	) (*peerReputationObserver, error) {
		return nil, errors.New("sequence")
	}
	if _, err := openSearchLearningStores(context.Background(), learningVault(t)); err == nil {
		t.Fatal("peer observation open failure did not surface")
	}
}

func learningVault(t *testing.T) *vault.Vault {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	return storage
}
