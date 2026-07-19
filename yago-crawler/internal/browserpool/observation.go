package browserpool

import "time"

type FailureReason string

const (
	FailureSlotDeadline FailureReason = "slot_deadline"
	FailureCooldown     FailureReason = "cooldown"
	FailureLaunch       FailureReason = "launch"
	FailureRender       FailureReason = "render"
)

func FailureReasons() [4]FailureReason {
	return [...]FailureReason{
		FailureSlotDeadline,
		FailureCooldown,
		FailureLaunch,
		FailureRender,
	}
}

func (reason FailureReason) Recognized() bool {
	for _, known := range FailureReasons() {
		if reason == known {
			return true
		}
	}

	return false
}

type SessionState string

const (
	SessionReady   SessionState = "ready"
	SessionBusy    SessionState = "busy"
	SessionCooling SessionState = "cooling"
)

func SessionStates() [3]SessionState {
	return [...]SessionState{
		SessionReady,
		SessionBusy,
		SessionCooling,
	}
}

type State struct {
	Ready   int
	Busy    int
	Cooling int
}

type Observer interface {
	ObserveBrowserSlotWait(time.Duration)
	ObserveBrowserPoolState(State)
	ObserveBrowserFailure(FailureReason)
}
