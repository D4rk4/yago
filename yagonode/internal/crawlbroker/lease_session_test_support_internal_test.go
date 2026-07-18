package crawlbroker

import (
	"context"
	"testing"
)

const testWorkerSessionID = "test-worker-session"

func leaseOneForSession(
	t *testing.T,
	queue *DurableOrderQueue,
	name string,
	workerID string,
	workerSessionID string,
) string {
	t.Helper()
	if err := queue.Publish(t.Context(), testOrder(name)); err != nil {
		t.Fatalf("publish %s: %v", name, err)
	}
	_, leaseID, found, err := queue.leasePopForSession(
		t.Context(),
		workerID,
		workerSessionID,
	)
	if err != nil || !found {
		t.Fatalf("lease %s: found=%v err=%v", name, found, err)
	}

	return leaseID
}

func activateTestWorkerSession(
	t *testing.T,
	server *exchangeServer,
	workerID string,
	workerSessionID string,
) {
	t.Helper()
	if _, _, err := server.activateWorkerSession(
		context.Background(),
		workerID,
		workerSessionID,
		func() {},
	); err != nil {
		t.Fatalf("activate worker session: %v", err)
	}
}

func deactivateTestWorkerSession(
	t *testing.T,
	server *exchangeServer,
	workerSessionID string,
) {
	t.Helper()
	workerID := "worker"
	current := server.sessions.registration(workerID)
	if current.id != workerSessionID || !current.connected {
		t.Fatalf("active worker session = %+v, want %s", current, workerSessionID)
	}
	server.sessions.deactivate(workerID, workerSessionID, current.generation)
}
