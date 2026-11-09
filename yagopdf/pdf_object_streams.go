package yagopdf

import (
	"bytes"
	"cmp"
	"slices"
	"strconv"
)

const pdfMaxUnpackedObjectStreamBytes = 8 << 20

type pdfObjectStream struct {
	encoded pdfEncodedStream
	end     int
}

func pdfWithUnpackedObjectStreams(body []byte, quota *pdfDecodeQuota) []byte {
	budget := newPDFDecodeQuota(min(quota.remainingBytes, pdfMaxUnpackedObjectStreamBytes))
	available := budget.remainingBytes
	var unpacked bytes.Buffer
	copied := 0
	members := 0
	for _, stream := range pdfObjectStreamsInFileOrder(body) {
		decoded, ok := budget.decode(stream.encoded)
		if !ok {
			continue
		}
		objects := pdfObjectStreamMembers(
			stream.encoded.dictionary,
			decoded,
			pdfMaxIndirectObjects-members,
		)
		if len(objects) == 0 {
			continue
		}
		_, _ = unpacked.Write(body[copied:stream.end])
		copied = stream.end
		for _, object := range objects {
			_, _ = unpacked.WriteString("\n" + object.reference + " obj\n")
			_, _ = unpacked.Write(object.value)
			_, _ = unpacked.WriteString("\nendobj\n")
		}
		members += len(objects)
	}
	quota.consume(available - budget.remainingBytes)
	if copied == 0 {
		return body
	}
	_, _ = unpacked.Write(body[copied:])

	return unpacked.Bytes()
}

func pdfObjectStreamsInFileOrder(body []byte) []pdfObjectStream {
	streams := make([]pdfObjectStream, 0, 8)
	for _, object := range pdfScanIndirectObjects(body) {
		encoded, isStream := pdfEncodedStreamOf(object.value)
		if isStream && pdfObjectStreamDictionary(encoded.dictionary) {
			streams = append(streams, pdfObjectStream{encoded: encoded, end: object.end})
		}
	}
	slices.SortFunc(streams, func(left, right pdfObjectStream) int {
		return cmp.Compare(left.end, right.end)
	})

	return streams
}

func pdfObjectStreamDictionary(dictionary []byte) bool {
	value := pdfDictionaryEntryValue(dictionary, "Type")
	if len(value) == 0 || value[0] != '/' {
		return false
	}
	name, _ := pdfName(value)

	return name == "ObjStm"
}

func pdfObjectStreamMembers(dictionary []byte, decoded []byte, limit int) []pdfIndirectObject {
	count, _, countFound := pdfObjectStreamInteger(pdfDictionaryEntryValue(dictionary, "N"), 0)
	first, _, firstFound := pdfObjectStreamInteger(pdfDictionaryEntryValue(dictionary, "First"), 0)
	if !countFound || !firstFound || first > len(decoded) {
		return nil
	}
	numbers, offsets := pdfObjectStreamHeader(
		decoded[:first],
		min(count, limit+1),
		len(decoded)-first,
	)
	members := make([]pdfIndirectObject, 0, min(len(numbers), limit))
	for index := 0; index < len(numbers) && index < limit; index++ {
		end := len(decoded)
		if index+1 < len(offsets) {
			end = first + offsets[index+1]
		}
		members = append(members, pdfIndirectObject{
			reference: strconv.Itoa(numbers[index]) + " 0",
			value:     bytes.Trim(decoded[first+offsets[index]:end], "\x00\t\n\f\r "),
		})
	}

	return members
}

func pdfObjectStreamHeader(header []byte, limit int, maximumOffset int) ([]int, []int) {
	numbers := make([]int, 0, min(limit, 64))
	offsets := make([]int, 0, min(limit, 64))
	at := 0
	for len(numbers) < limit {
		number, afterNumber, numberFound := pdfObjectStreamInteger(header, at)
		offset, afterOffset, offsetFound := pdfObjectStreamInteger(header, afterNumber)
		if !numberFound || !offsetFound || offset > maximumOffset ||
			len(offsets) > 0 && offset < offsets[len(offsets)-1] {
			break
		}
		numbers = append(numbers, number)
		offsets = append(offsets, offset)
		at = afterOffset
	}

	return numbers, offsets
}

func pdfObjectStreamInteger(value []byte, at int) (int, int, bool) {
	for at < len(value) && pdfWhiteSpace(value[at]) {
		at++
	}
	start := at
	for at < len(value) && value[at] >= '0' && value[at] <= '9' {
		at++
	}
	integer, err := strconv.Atoi(string(value[start:at]))

	return integer, at, err == nil
}
