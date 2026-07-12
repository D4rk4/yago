package faviconproxy

import (
	"container/list"
	"reflect"
)

const retainedIconMapEntryBytes = 64

var (
	retainedIconHostWidth        = int(reflect.TypeOf(hostEntry{}).Size())
	retainedIconBodyWidth        = int(reflect.TypeOf(iconBody{}).Size())
	retainedIconListElementWidth = int(reflect.TypeOf(list.Element{}).Size())
)

func retainedIconHostBytes(host string, contentType string) int {
	return retainedIconHostWidth + retainedIconListElementWidth +
		retainedIconMapEntryBytes + len(host) + len(contentType)
}

func retainedIconBodyBytes(length int) int {
	return retainedIconBodyWidth + retainedIconMapEntryBytes + length
}
