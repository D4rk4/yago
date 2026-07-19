package yagoproto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

const DefaultNetwork = "freeworld"

var networkSaltEntropy io.Reader = rand.Reader

type NetworkAuthenticationMode string

const (
	NetworkAuthenticationUncontrolled NetworkAuthenticationMode = "uncontrolled"
	NetworkAuthenticationSaltedMagic  NetworkAuthenticationMode = "salted-magic-sim"
)

type NetworkAccess struct {
	NetworkName string
	Mode        NetworkAuthenticationMode
	Essentials  string
	Self        yagomodel.Hash
}

func NetworkUnit(name string) string {
	if name == "" {
		return DefaultNetwork
	}

	return name
}

func MagicMD5(key, iam, essentials string) string {
	return yagomodel.YaCyHashHex(key + iam + essentials)
}

func (a NetworkAccess) Authorizes(form url.Values) bool {
	if NetworkUnit(form.Get(FieldNetworkName)) != NetworkUnit(a.NetworkName) {
		return false
	}
	if a.Mode == "" || a.Mode == NetworkAuthenticationUncontrolled {
		return true
	}
	if a.Mode != NetworkAuthenticationSaltedMagic {
		return false
	}
	expected := MagicMD5(form.Get(FieldKey), form.Get(FieldIam), a.Essentials)
	provided := form.Get(FieldMagicMD5)

	return len(provided) == len(expected) && subtle.ConstantTimeCompare(
		[]byte(provided),
		[]byte(expected),
	) == 1
}

func (a NetworkAccess) Sign(form url.Values) error {
	salt, err := networkSalt()
	if err != nil {
		return err
	}
	a.SignWithSalt(form, salt)

	return nil
}

func (a NetworkAccess) SignWithSalt(form url.Values, salt string) {
	form.Set(FieldNetworkName, NetworkUnit(a.NetworkName))
	form.Set(FieldIam, a.Self.String())
	form.Set(FieldKey, salt)
	if a.Mode == NetworkAuthenticationSaltedMagic {
		form.Set(FieldMagicMD5, MagicMD5(salt, a.Self.String(), a.Essentials))
	} else {
		form.Del(FieldMagicMD5)
	}
}

func networkSalt() (string, error) {
	var value [6]byte
	if _, err := io.ReadFull(networkSaltEntropy, value[:]); err != nil {
		return "", fmt.Errorf("create network authentication salt: %w", err)
	}

	return base64.StdEncoding.EncodeToString(value[:]), nil
}
