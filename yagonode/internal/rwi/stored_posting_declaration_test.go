package rwi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const declaredBytesBeyondRow = 1 << 20

func storedPostingWithColumnMask(columns ...string) *bytes.Buffer {
	var data bytes.Buffer
	data.WriteByte(storedPostingFormatV1)
	var mask uint32
	for _, column := range columns {
		mask |= 1 << uint(storedPostingColumnIndex[column])
	}
	var maskBytes [4]byte
	binary.LittleEndian.PutUint32(maskBytes[:], mask)
	data.Write(maskBytes[:])

	return &data
}

func storedPostingWithOversizedColumn() []byte {
	data := storedPostingWithColumnMask(yagomodel.ColLanguage)
	writeUvarint(data, declaredBytesBeyondRow)

	return data.Bytes()
}

func storedPostingWithOversizedExtrasCount() []byte {
	data := storedPostingWithColumnMask()
	writeUvarint(data, declaredBytesBeyondRow)

	return data.Bytes()
}

func storedPostingWithOversizedExtraValue() []byte {
	data := storedPostingWithColumnMask()
	writeUvarint(data, 1)
	writeLengthPrefixed(data, []byte("k"))
	writeUvarint(data, declaredBytesBeyondRow)

	return data.Bytes()
}

// TestDecodeStoredPostingRejectsDeclarationsLongerThanTheRow pins the refusal a
// peer-supplied index row earns when its own length declaration cannot fit in the
// bytes it carries. The declaration is attacker-controlled: with the bound gone
// the decoder reserves whatever the row asks for, keeps the zero padding that
// never arrived, and reports a posting nobody sent, so the refusal must name the
// oversized declaration rather than surface as a generic truncated read.
func TestDecodeStoredPostingRejectsDeclarationsLongerThanTheRow(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
	}{
		{name: "column value", value: storedPostingWithOversizedColumn()},
		{name: "extras count", value: storedPostingWithOversizedExtrasCount()},
		{name: "extra value", value: storedPostingWithOversizedExtraValue()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoded, err := decodeStoredPosting("ABCDEFGHIJKL", test.value)
			if !errors.Is(err, yagomodel.ErrBadRWIPosting) {
				t.Fatalf("err = %v, want ErrBadRWIPosting", err)
			}
			if !strings.Contains(err.Error(), "exceeds remaining bytes") {
				t.Fatalf("err = %v, want the oversized declaration named", err)
			}
			if len(decoded.Properties) != 0 {
				t.Fatalf("decoded = %+v, want nothing from a refused row", decoded)
			}
		})
	}
}
