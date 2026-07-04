package yagoproto

import "fmt"

type TransferRWIResult string

type TransferURLResult string

const (
	ResultOK          = "ok"
	ResultWrongTarget = "wrong_target"
)

const (
	ResultBusy            TransferRWIResult = "busy"
	ResultNotGranted      TransferRWIResult = "not_granted"
	ResultTooHighLoad     TransferRWIResult = "too high load"
	ResultPostOrEnvIsNull TransferRWIResult = "post or env is null!"
	ResultNotAuthentified TransferRWIResult = "not authentified"
	ResultMissingWordC    TransferRWIResult = "missing wordc"
	ResultMissingEntryC   TransferRWIResult = "missing entryc"
	ResultMissingIndexes  TransferRWIResult = "missing indexes"
)

const ResultErrorNotGranted TransferURLResult = "error_not_granted"

func parseTransferRWIResult(raw string) (TransferRWIResult, error) {
	result := TransferRWIResult(raw)
	switch result {
	case TransferRWIResult(ResultOK),
		TransferRWIResult(ResultWrongTarget),
		ResultBusy,
		ResultNotGranted,
		ResultTooHighLoad,
		ResultPostOrEnvIsNull,
		ResultNotAuthentified,
		ResultMissingWordC,
		ResultMissingEntryC,
		ResultMissingIndexes:
		return result, nil
	default:
		return "", fmt.Errorf("%w: transferRWI response %s=%q", ErrBadField, FieldResult, raw)
	}
}

func parseTransferURLResult(raw string) (TransferURLResult, error) {
	if raw == "" {
		return "", nil
	}

	result := TransferURLResult(raw)
	switch result {
	case TransferURLResult(ResultOK),
		TransferURLResult(ResultWrongTarget),
		ResultErrorNotGranted:
		return result, nil
	default:
		return "", fmt.Errorf("%w: transferURL response %s=%q", ErrBadField, FieldResult, raw)
	}
}
