package documentstore

import (
	"context"
	"fmt"
	"math"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (a *storedDocumentAdmissionKeys) reserve(
	ctx context.Context,
	requested uint64,
) (uint64, uint64, error) {
	extension := max(uint64(storedDocumentAdmissionReservation), requested)
	highWater := uint64(0)
	reservationFloor := uint64(0)
	err := a.vault.Update(ctx, func(tx *vault.Txn) error {
		durable, found, err := a.admissions.Get(tx, documentAdmissionHighWaterKey)
		if err != nil {
			return fmt.Errorf("read durable document admission: %w", err)
		}
		if !found {
			durable = 0
		}
		base := max(durable, a.reserved)
		if highWater == 0 || base > highWater {
			if extension > math.MaxUint64-base {
				return fmt.Errorf("ordered document admissions exhausted")
			}
			reservationFloor = base
			highWater = base + extension
		}

		return a.admissions.Put(tx, documentAdmissionHighWaterKey, highWater)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("reserve document admissions: %w", err)
	}

	return reservationFloor, highWater, nil
}
