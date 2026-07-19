package yagoproto

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

type failingNetworkSaltEntropy struct{}

func (failingNetworkSaltEntropy) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestNetworkAccessSignFailsClosedWhenEntropyIsUnavailable(t *testing.T) {
	original := networkSaltEntropy
	networkSaltEntropy = failingNetworkSaltEntropy{}
	t.Cleanup(func() { networkSaltEntropy = original })

	form := url.Values{"retained": {"value"}}
	err := (NetworkAccess{Mode: NetworkAuthenticationSaltedMagic}).Sign(form)
	if err == nil || !strings.Contains(err.Error(), "create network authentication salt") {
		t.Fatalf("Sign error = %v", err)
	}
	if form.Get("retained") != "value" || form.Has(FieldKey) ||
		form.Has(FieldMagicMD5) || form.Has(FieldNetworkName) || form.Has(FieldIam) {
		t.Fatalf("form changed after entropy failure: %v", form)
	}
}
