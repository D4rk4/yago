package httpguard

import (
	"fmt"
	"net/http"
	"strings"
)

func writeResponseText(
	w http.ResponseWriter,
	body string,
) error {
	_, err := strings.NewReader(body).WriteTo(w)
	if err != nil {
		return fmt.Errorf("write response text: %w", err)
	}
	return nil
}
