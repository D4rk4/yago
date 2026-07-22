package peermessage

import (
	"strings"

	"github.com/D4rk4/yago/yagoproto"
)

const acceptedSubjectSize = yagoproto.MessageSubjectMaximumBytes

func messageContentAdmitted(subject, body string) bool {
	return strings.TrimSpace(subject) != "" && strings.TrimSpace(body) != "" &&
		len(subject) <= acceptedSubjectSize && len(body) <= acceptedMessageSize
}
