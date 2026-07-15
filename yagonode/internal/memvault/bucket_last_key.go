package memvault

import "github.com/D4rk4/yago/yagonode/internal/vault"

func (b memBucket) LastKey() (vault.Key, error) {
	var last string
	found := false
	for key := range b.entries {
		if !found || key > last {
			last = key
			found = true
		}
	}
	if !found {
		return nil, nil
	}

	return vault.Key(last), nil
}
