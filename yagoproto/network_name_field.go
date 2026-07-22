package yagoproto

import "net/url"

func putNetworkName(form url.Values, name string, present bool) {
	if name != "" || present {
		form.Set(FieldNetworkName, name)
	}
}

func parseNetworkName(form url.Values) (string, bool) {
	return form.Get(FieldNetworkName), form.Has(FieldNetworkName)
}
