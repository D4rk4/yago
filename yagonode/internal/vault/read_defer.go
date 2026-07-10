package vault

import "time"

// readDeferrer is the optional engine capability behind the vault's read-defer
// controls (IO-PRIO-01 / PERF-PRIO-02): the sharded engine yields bulk writes to
// in-flight interactive reads and records how long it yielded. The in-memory
// engine writes to RAM with nothing to yield, so it does not implement it and
// the methods below are no-ops there.
type readDeferrer interface {
	SetReadDeferBudget(budget time.Duration)
	ReadDeferred() time.Duration
}

// SetReadDeferBudget sets how long the engine yields a bulk write to in-flight
// interactive reads (PERF-PRIO-02). The node calls it once at boot with the
// operator's restart-required setting; it is a no-op on engines that do not
// defer.
func (v *Vault) SetReadDeferBudget(budget time.Duration) {
	if v == nil || v.engine == nil {
		return
	}
	if deferrer, ok := v.engine.(readDeferrer); ok {
		deferrer.SetReadDeferBudget(budget)
	}
}

// ReadDeferred reports the cumulative time the engine has yielded writes to
// in-flight reads (PERF-PRIO-02), or zero on engines that do not defer.
func (v *Vault) ReadDeferred() time.Duration {
	if v == nil || v.engine == nil {
		return 0
	}
	deferrer, ok := v.engine.(readDeferrer)
	if !ok {
		return 0
	}

	return deferrer.ReadDeferred()
}
