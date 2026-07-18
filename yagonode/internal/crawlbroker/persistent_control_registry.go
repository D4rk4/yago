package crawlbroker

import "github.com/D4rk4/yago/yagonode/internal/vault"

func newPersistentControlRegistry(
	storage *vault.Vault,
	defaults ...crawlerControlDefaults,
) (*ControlRegistry, error) {
	directives, err := newPersistentControlDirectiveLedger(storage)
	if err != nil {
		return nil, err
	}

	return newControlRegistryWithLedger(directives, defaults...), nil
}
