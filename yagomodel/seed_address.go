package yagomodel

func (s Seed) SharesAddress(other Seed) bool {
	addresses := s.addressSet()
	if len(addresses) == 0 {
		return false
	}

	for address := range other.addresses() {
		if _, ok := addresses[address]; ok {
			return true
		}
	}

	return false
}

func (s Seed) addressSet() map[string]struct{} {
	addresses := map[string]struct{}{}
	for address := range s.addresses() {
		addresses[address] = struct{}{}
	}

	return addresses
}

func (s Seed) addresses() func(func(string) bool) {
	return func(yield func(string) bool) {
		if host, ok := s.IP.Get(); ok {
			if !yield(host.String()) {
				return
			}
		}
		if hosts, ok := s.IP6.Get(); ok {
			for _, host := range hosts {
				if !yield(host.String()) {
					return
				}
			}
		}
	}
}
