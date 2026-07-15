package vault

type vaultEngineLease struct {
	vault  *Vault
	engine Engine
}

func (v *Vault) acquireEngineLease() (vaultEngineLease, error) {
	if v == nil {
		return vaultEngineLease{}, errVaultClosed
	}
	v.lifecycle.RLock()
	if v.engine == nil {
		v.lifecycle.RUnlock()

		return vaultEngineLease{}, errVaultClosed
	}

	return vaultEngineLease{vault: v, engine: v.engine}, nil
}

func (l vaultEngineLease) release() {
	l.vault.lifecycle.RUnlock()
}
