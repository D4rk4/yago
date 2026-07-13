package publicweb

type permanentAdmissionError struct {
	cause error
}

func (e permanentAdmissionError) Error() string {
	return e.cause.Error()
}

func (e permanentAdmissionError) Unwrap() error {
	return e.cause
}

func (permanentAdmissionError) Permanent() bool {
	return true
}

func markPermanentAdmissionFailure(err error) error {
	return permanentAdmissionError{cause: err}
}
