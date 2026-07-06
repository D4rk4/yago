package crawlcapability

type TransientPublicationError struct {
	Err error
}

func (e TransientPublicationError) Error() string {
	if e.Err == nil {
		return "transient publication"
	}
	return "transient publication: " + e.Err.Error()
}

func (e TransientPublicationError) Unwrap() error {
	return e.Err
}
