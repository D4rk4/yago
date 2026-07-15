package documentstore

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const storedDocumentAdmissionReservation = 256

type storedDocumentAdmissionKeys struct {
	vault      *vault.Vault
	admissions *vault.Keyspace[uint64]
	mutex      sync.Mutex
	issued     uint64
	reserved   uint64
}

func openStoredDocumentAdmissionKeys(
	v *vault.Vault,
	admissions *vault.Keyspace[uint64],
) (*storedDocumentAdmissionKeys, error) {
	persisted, physical, err := loadStoredDocumentAdmissionHighWater(v, admissions)
	if err != nil {
		return nil, err
	}
	highWater := max(persisted, physical)
	if highWater > persisted {
		highWater, err = persistRecoveredStoredDocumentAdmissionHighWater(
			v,
			admissions,
			highWater,
		)
		if err != nil {
			return nil, err
		}
	}

	return &storedDocumentAdmissionKeys{
		vault:      v,
		admissions: admissions,
		issued:     highWater,
		reserved:   highWater,
	}, nil
}

func (a *storedDocumentAdmissionKeys) issue(
	ctx context.Context,
	total int,
) ([]uint64, error) {
	if total < 1 {
		return nil, nil
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	requested := uint64(total)
	available := a.reserved - a.issued
	if available < requested {
		reservationFloor, highWater, err := a.reserve(ctx, requested)
		if err != nil {
			return nil, err
		}
		if reservationFloor > a.issued {
			a.issued = reservationFloor
		}
		a.reserved = highWater
	}
	issued := make([]uint64, total)
	for index := range issued {
		a.issued++
		issued[index] = a.issued
	}

	return issued, nil
}
