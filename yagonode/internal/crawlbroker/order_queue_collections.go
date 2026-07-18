package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type orderQueueCollections struct {
	orders                   *vault.Collection[[]byte]
	normalOrderIndex         *vault.Collection[[]byte]
	automaticOrderIndex      *vault.Collection[[]byte]
	sequence                 *vault.Collection[uint64]
	idempotencyKeys          *vault.Collection[uint64]
	leases                   *vault.Collection[leaseRecord]
	leaseSettlements         *vault.Collection[leaseSettlementRecord]
	leaseSettlementOrder     *vault.Collection[[]byte]
	leaseSettlementExpiry    *vault.Collection[[]byte]
	leaseControlTargets      *vault.Collection[leaseControlTarget]
	completedControlTargets  *vault.Collection[leaseControlTarget]
	terminalSettlementSecret []byte
}

func registerOrderQueueCollections(storage *vault.Vault) (orderQueueCollections, error) {
	collections, err := registerPendingOrderCollections(storage)
	if err != nil {
		return orderQueueCollections{}, err
	}
	if err := registerLeaseCollections(storage, &collections); err != nil {
		return orderQueueCollections{}, err
	}
	if err := registerLeaseControlCollections(storage, &collections); err != nil {
		return orderQueueCollections{}, err
	}

	return collections, nil
}

func registerPendingOrderCollections(storage *vault.Vault) (orderQueueCollections, error) {
	var collections orderQueueCollections
	var err error
	collections.orders, err = vault.Register(storage, orderBucket, orderCodec{})
	if err != nil {
		return orderQueueCollections{}, fmt.Errorf("register crawl order queue: %w", err)
	}
	collections.normalOrderIndex, err = vault.Register(
		storage,
		normalOrderIndexBucket,
		orderCodec{},
	)
	if err != nil {
		return orderQueueCollections{}, fmt.Errorf("register normal crawl order index: %w", err)
	}
	collections.automaticOrderIndex, err = vault.Register(
		storage,
		automaticOrderIndexBucket,
		orderCodec{},
	)
	if err != nil {
		return orderQueueCollections{}, fmt.Errorf("register automatic crawl order index: %w", err)
	}
	collections.sequence, err = vault.Register(storage, seqBucket, sequenceCodec{})
	if err != nil {
		return orderQueueCollections{}, fmt.Errorf("register crawl order sequence: %w", err)
	}
	collections.idempotencyKeys, err = vault.Register(storage, idempotencyBucket, sequenceCodec{})
	if err != nil {
		return orderQueueCollections{}, fmt.Errorf("register crawl order idempotency keys: %w", err)
	}

	return collections, nil
}

func registerLeaseCollections(storage *vault.Vault, collections *orderQueueCollections) error {
	var err error
	collections.leases, err = vault.Register(storage, leaseBucket, leaseRecordCodec{})
	if err != nil {
		return fmt.Errorf("register crawl order leases: %w", err)
	}
	collections.leaseSettlements, err = vault.Register(
		storage,
		leaseSettlementBucket,
		leaseSettlementRecordCodec{},
	)
	if err != nil {
		return fmt.Errorf("register crawl lease settlements: %w", err)
	}
	collections.leaseSettlementOrder, err = vault.Register(
		storage,
		leaseSettlementOrderBucket,
		leaseSettlementIdentityCodec{},
	)
	if err != nil {
		return fmt.Errorf("register crawl lease settlement index: %w", err)
	}
	collections.leaseSettlementExpiry, err = vault.Register(
		storage,
		leaseSettlementExpiryBucket,
		leaseSettlementIdentityCodec{},
	)
	if err != nil {
		return fmt.Errorf("register crawl lease settlement expiry: %w", err)
	}

	return nil
}

func registerLeaseControlCollections(
	storage *vault.Vault,
	collections *orderQueueCollections,
) error {
	var err error
	collections.leaseControlTargets, err = vault.Register(
		storage,
		leaseControlTargetBucket,
		leaseControlTargetCodec{},
	)
	if err != nil {
		return fmt.Errorf("register crawl lease control targets: %w", err)
	}
	collections.completedControlTargets, err = vault.Register(
		storage,
		completedLeaseControlTargetBucket,
		leaseControlTargetCodec{},
	)
	if err != nil {
		return fmt.Errorf("register completed crawl lease control targets: %w", err)
	}
	secrets, err := vault.Register(storage, terminalSettlementSecretBucket, orderCodec{})
	if err != nil {
		return fmt.Errorf("register terminal settlement secret: %w", err)
	}
	collections.terminalSettlementSecret, err = loadTerminalSettlementSecret(storage, secrets)
	if err != nil {
		return fmt.Errorf("initialize terminal settlement secret: %w", err)
	}

	return nil
}
