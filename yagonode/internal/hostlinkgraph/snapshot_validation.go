package hostlinkgraph

import (
	"encoding/json"
	"errors"
)

const (
	SnapshotHostHashBytes            = 6
	MaximumSnapshotLinkedHosts       = 4096
	MaximumSnapshotReferencesPerHost = 64
	MaximumSnapshotReferences        = 32768
	MaximumSnapshotReferenceBytes    = 512
)

var (
	errSnapshotRowDefinition        = errors.New("host-link snapshot row definition is invalid")
	errSnapshotLinkedHosts          = errors.New("host-link snapshot has too many linked hosts")
	errSnapshotHostHash             = errors.New("host-link snapshot host hash is invalid")
	errSnapshotDuplicateHost        = errors.New("host-link snapshot contains a duplicate host")
	errSnapshotHostReferences       = errors.New("host-link snapshot host references are invalid")
	errSnapshotReferences           = errors.New("host-link snapshot has too many references")
	errSnapshotReferenceEmpty       = errors.New("host-link snapshot reference is empty")
	errSnapshotReferenceTooLarge    = errors.New("host-link snapshot reference is too large")
	errSnapshotReferenceInvalidJSON = errors.New("host-link snapshot reference is invalid JSON")
)

func ValidateSnapshot(graph Graph) error {
	if graph.RowDefinition != HostReferenceRowDefinition {
		return errSnapshotRowDefinition
	}
	if len(graph.LinkedHosts) > MaximumSnapshotLinkedHosts {
		return errSnapshotLinkedHosts
	}

	linkedHosts := make(map[string]struct{}, len(graph.LinkedHosts))
	references := 0
	for _, linkedHost := range graph.LinkedHosts {
		if err := validateSnapshotLinkedHost(linkedHost, linkedHosts); err != nil {
			return err
		}

		references += len(linkedHost.References)
		if references > MaximumSnapshotReferences {
			return errSnapshotReferences
		}
		for _, reference := range linkedHost.References {
			if err := validateSnapshotReference(reference); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateSnapshotLinkedHost(
	linkedHost LinkedHost,
	linkedHosts map[string]struct{},
) error {
	if len(linkedHost.HostHash) != SnapshotHostHashBytes {
		return errSnapshotHostHash
	}
	if _, exists := linkedHosts[linkedHost.HostHash]; exists {
		return errSnapshotDuplicateHost
	}
	linkedHosts[linkedHost.HostHash] = struct{}{}
	if len(linkedHost.References) == 0 ||
		len(linkedHost.References) > MaximumSnapshotReferencesPerHost {
		return errSnapshotHostReferences
	}

	return nil
}

func validateSnapshotReference(reference json.RawMessage) error {
	if len(reference) == 0 {
		return errSnapshotReferenceEmpty
	}
	if len(reference) > MaximumSnapshotReferenceBytes {
		return errSnapshotReferenceTooLarge
	}
	if !json.Valid(reference) {
		return errSnapshotReferenceInvalidJSON
	}

	return nil
}
