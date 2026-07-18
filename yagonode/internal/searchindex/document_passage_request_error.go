package searchindex

type documentPassageRequestError struct {
	message string
}

func (e documentPassageRequestError) Error() string {
	return e.message
}

func (documentPassageRequestError) DocumentPassageRequestInvalid() {}

func invalidDocumentPassageRequest(message string) error {
	return documentPassageRequestError{message: message}
}
