package peermessage

import (
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const messagesBucket vault.Name = "peermessage"

type messageCodec struct{}

func (messageCodec) Encode(message Message) ([]byte, error) {
	raw, _ := json.Marshal(message)
	return raw, nil
}

func (messageCodec) Decode(raw []byte) (Message, error) {
	var message Message
	if err := json.Unmarshal(raw, &message); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}

	return message, nil
}

func registerMessages(v *vault.Vault) (*vault.Collection[Message], error) {
	messages, err := vault.Register(v, messagesBucket, messageCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer messages: %w", err)
	}

	return messages, nil
}
