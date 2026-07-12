package yagomodel

import (
	"reflect"
	"strings"
)

const (
	maximumSeedPlainBytes      = 32 << 10
	maximumSeedProperties      = 128
	maximumSeedPropertyKey     = 128
	maximumSeedPropertyValue   = 8 << 10
	maximumSeedNameBytes       = 256
	maximumSeedNewsBytes       = 8 << 10
	seedPropertyRetentionBytes = 64
)

var (
	seedRetentionWidth = int(reflect.TypeOf(Seed{}).Size())
	hostRetentionWidth = int(reflect.TypeOf(Host{}).Size())
)

func seedPropertyLimit(key string) int {
	if key == SeedName {
		return maximumSeedNameBytes
	}
	if key == SeedNews {
		return maximumSeedNewsBytes
	}

	return maximumSeedPropertyValue
}

func (s Seed) Copy() Seed {
	copied := s
	copied.Hash = Hash(strings.Clone(s.Hash.String()))
	copied.Name = copyOptionalString(s.Name)
	copied.IP = copyOptionalHost(s.IP)
	copied.IP6 = copyOptionalHosts(s.IP6)
	copied.PeerType = copyOptionalStringValue(s.PeerType)
	copied.Version = copyOptionalStringValue(s.Version)
	copied.UTC = copyOptionalStringValue(s.UTC)
	copied.News = copyOptionalString(s.News)
	if s.customProperties != nil {
		copied.customProperties = make(map[string]string, len(s.customProperties))
		for key, value := range s.customProperties {
			copied.customProperties[strings.Clone(key)] = strings.Clone(value)
		}
	}

	return copied
}

func (s Seed) RetainedBytes() int {
	total := seedRetentionWidth + len(s.Hash)
	total += optionalStringBytes(s.Name)
	total += optionalHostBytes(s.IP)
	if hosts, ok := s.IP6.Get(); ok {
		total += len(hosts) * hostRetentionWidth
		for _, host := range hosts {
			total += len(host.hostname)
		}
	}
	total += optionalStringValueBytes(s.PeerType)
	total += optionalStringValueBytes(s.Version)
	total += optionalStringValueBytes(s.UTC)
	total += optionalStringBytes(s.News)
	for key, value := range s.customProperties {
		total += seedPropertyRetentionBytes + len(key) + len(value)
	}

	return total
}

func copyOptionalString(value Optional[string]) Optional[string] {
	stored, ok := value.Get()
	if !ok {
		return None[string]()
	}

	return Some(strings.Clone(stored))
}

func copyOptionalStringValue[T ~string](value Optional[T]) Optional[T] {
	stored, ok := value.Get()
	if !ok {
		return None[T]()
	}

	return Some(T(strings.Clone(string(stored))))
}

func copyOptionalHost(value Optional[Host]) Optional[Host] {
	stored, ok := value.Get()
	if !ok {
		return None[Host]()
	}
	stored.hostname = strings.Clone(stored.hostname)

	return Some(stored)
}

func copyOptionalHosts(value Optional[[]Host]) Optional[[]Host] {
	stored, ok := value.Get()
	if !ok {
		return None[[]Host]()
	}
	copied := make([]Host, len(stored))
	for index, host := range stored {
		host.hostname = strings.Clone(host.hostname)
		copied[index] = host
	}

	return Some(copied)
}

func optionalStringBytes(value Optional[string]) int {
	stored, _ := value.Get()

	return len(stored)
}

func optionalStringValueBytes[T ~string](value Optional[T]) int {
	stored, _ := value.Get()

	return len(stored)
}

func optionalHostBytes(value Optional[Host]) int {
	stored, ok := value.Get()
	if !ok {
		return 0
	}

	return hostRetentionWidth + len(stored.hostname)
}
