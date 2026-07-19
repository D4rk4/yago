package fleetfetchstart

import "sync"

type SessionGate struct {
	mutex      sync.Mutex
	connected  bool
	generation uint64
	changed    chan struct{}
}

func NewSessionGate() *SessionGate {
	return &SessionGate{changed: make(chan struct{})}
}

func (gate *SessionGate) Connected() {
	gate.setConnected(true)
}

func (gate *SessionGate) Disconnected() {
	gate.setConnected(false)
}

func (gate *SessionGate) Snapshot() (bool, uint64, <-chan struct{}) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()

	return gate.connected, gate.generation, gate.changed
}

func (gate *SessionGate) setConnected(connected bool) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.connected == connected {
		return
	}
	gate.connected = connected
	gate.generation++
	close(gate.changed)
	gate.changed = make(chan struct{})
}
