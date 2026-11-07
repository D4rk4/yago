package yagonode

func (w nodePeerWire) close() {
	w.lifecycle.Close()
	closeVault(w.rosterVault.dedicated)
}
