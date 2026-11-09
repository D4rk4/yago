package yagopdf

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	hostileAllocationCeiling   = 512 << 20
	hostileNaiveExpansionBytes = 1 << 30
	hostileRepeatedChunkBytes  = 1 << 20
	adler32Modulus             = 65521
)

var (
	hostileZeroBomb = sync.OnceValue(func() []byte {
		return syntheticRepeatedZlib([]byte{0}, hostileNaiveExpansionBytes)
	})
	hostileTextOperatorBomb = sync.OnceValue(func() []byte {
		return syntheticRepeatedZlib([]byte(hostileTextBombOperators), hostileNaiveExpansionBytes)
	})
	hostileLZWZerosBeyondTheStreamCap = sync.OnceValue(func() []byte {
		return syntheticLZW(make([]byte, pdfMaxStreamBytes+hostileRepeatedChunkBytes))
	})
)

type syntheticPDF struct {
	body bytes.Buffer
}

type measuredExtraction struct {
	document       Document
	readable       bool
	allocatedBytes uint64
}

func newSyntheticPDF() *syntheticPDF {
	pdf := &syntheticPDF{}
	pdf.body.WriteString("%PDF-1.7\n")

	return pdf
}

func (pdf *syntheticPDF) indirectObject(reference int, value string) *syntheticPDF {
	fmt.Fprintf(&pdf.body, "%d 0 obj\n%s\nendobj\n", reference, value)

	return pdf
}

func (pdf *syntheticPDF) encodedStream(
	reference int,
	dictionary string,
	encoded []byte,
) *syntheticPDF {
	fmt.Fprintf(
		&pdf.body,
		"%d 0 obj\n<< %s /Length %d >>\nstream\n",
		reference,
		dictionary,
		len(encoded),
	)
	pdf.body.Write(encoded)
	pdf.body.WriteString("\nendstream\nendobj\n")

	return pdf
}

func (pdf *syntheticPDF) rawStream(reference int, dictionary string, content string) *syntheticPDF {
	return pdf.encodedStream(reference, dictionary, []byte(content))
}

func (pdf *syntheticPDF) flateStream(
	reference int,
	dictionary string,
	content string,
) *syntheticPDF {
	return pdf.encodedStream(
		reference,
		"/Filter /FlateDecode "+dictionary,
		syntheticZlib([]byte(content)),
	)
}

func (pdf *syntheticPDF) objectStream(reference int, numbers []int, values []string) *syntheticPDF {
	var header, members strings.Builder
	for index, number := range numbers {
		fmt.Fprintf(&header, "%d %d ", number, members.Len())
		members.WriteString(values[index] + "\n")
	}

	return pdf.flateStream(reference,
		fmt.Sprintf("/Type /ObjStm /N %d /First %d", len(numbers), header.Len()),
		header.String()+members.String())
}

func (pdf *syntheticPDF) page(reference int, dictionary string) *syntheticPDF {
	return pdf.indirectObject(reference, "<< /Type /Page "+dictionary+" >>")
}

func (pdf *syntheticPDF) text(value string) *syntheticPDF {
	pdf.body.WriteString(value)

	return pdf
}

func (pdf *syntheticPDF) trailer(dictionary string) *syntheticPDF {
	pdf.body.WriteString("trailer\n" + dictionary + "\n")

	return pdf
}

func (pdf *syntheticPDF) finished() []byte {
	return append(bytes.Clone(pdf.body.Bytes()), "%%EOF\n"...)
}

func singlePagePDF(content string) []byte {
	return newSyntheticPDF().
		page(1, "/Contents 2 0 R").
		rawStream(2, "", content).
		finished()
}

func syntheticZlib(content []byte) []byte {
	var compressed bytes.Buffer
	writer, _ := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	_, _ = writer.Write(content)
	_ = writer.Close()

	return compressed.Bytes()
}

func syntheticLZW(content []byte) []byte {
	var compressed bytes.Buffer
	writer := lzw.NewWriter(&compressed, lzw.MSB, 8)
	_, _ = writer.Write(content)
	_ = writer.Close()

	return compressed.Bytes()
}

func syntheticASCII85(content []byte) []byte {
	var encoded bytes.Buffer
	writer := ascii85.NewEncoder(&encoded)
	_, _ = writer.Write(content)
	_ = writer.Close()
	encoded.WriteString("~>")

	return encoded.Bytes()
}

func syntheticASCIIHex(content []byte) []byte {
	return append([]byte(hex.EncodeToString(content)), '>')
}

func syntheticRepeatedZlib(pattern []byte, naiveBytes int) []byte {
	chunk := bytes.Repeat(pattern, max(1, hostileRepeatedChunkBytes/len(pattern)))
	chunks := max(2, (naiveBytes+len(chunk)-1)/len(chunk))
	var deflated bytes.Buffer
	writer, _ := zlib.NewWriterLevel(&deflated, zlib.BestCompression)
	_, _ = writer.Write(chunk)
	_ = writer.Flush()
	leadingBytes := deflated.Len()
	_, _ = writer.Write(chunk)
	_ = writer.Flush()
	repeatedSegment := bytes.Clone(deflated.Bytes()[leadingBytes:])
	stream := bytes.NewBuffer(make([]byte, 0, leadingBytes+chunks*len(repeatedSegment)+8))
	stream.Write(deflated.Bytes()[:leadingBytes])
	for range chunks - 1 {
		stream.Write(repeatedSegment)
	}
	stream.Write([]byte{0x03, 0x00})

	return binary.BigEndian.AppendUint32(stream.Bytes(), repeatedAdler32(chunk, chunks))
}

func repeatedAdler32(chunk []byte, repetitions int) uint32 {
	chunkSum, chunkWeightedSum := uint64(0), uint64(0)
	for index, value := range chunk {
		chunkSum = (chunkSum + uint64(value)) % adler32Modulus
		chunkWeightedSum = (chunkWeightedSum + uint64(len(chunk)-index)*uint64(value)) % adler32Modulus
	}
	low, high := uint64(1), uint64(0)
	chunkLength := uint64(len(chunk)) % adler32Modulus
	for range repetitions {
		high = (high + chunkLength*low + chunkWeightedSum) % adler32Modulus
		low = (low + chunkSum) % adler32Modulus
	}

	return uint32(high<<16 | low)
}

func readPDFMeasuringAllocation(body []byte) measuredExtraction {
	if !allocationMeasured {
		document, readable := ReadPDF(body)

		return measuredExtraction{document: document, readable: readable}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	document, readable := ReadPDF(body)
	runtime.ReadMemStats(&after)

	return measuredExtraction{
		document:       document,
		readable:       readable,
		allocatedBytes: after.TotalAlloc - before.TotalAlloc,
	}
}

func requireExtractionInvariants(t *testing.T, document Document, readable bool) {
	t.Helper()
	if len(document.Text) > pdfMaxTextBytes {
		t.Fatalf("text bytes = %d, above the %d byte cap", len(document.Text), pdfMaxTextBytes)
	}
	if len(document.Title) > pdfMaxTitleBytes {
		t.Fatalf("title bytes = %d, above the %d byte cap", len(document.Title), pdfMaxTitleBytes)
	}
	if !utf8.ValidString(document.Text) || !utf8.ValidString(document.Title) {
		t.Fatalf("extraction is not valid UTF-8: title %q text prefix %q",
			document.Title, document.Text[:min(len(document.Text), 64)])
	}
	if readable && document.Text == "" {
		t.Fatal("readable document carries no text")
	}
	if !readable && (document != Document{}) {
		t.Fatalf("unreadable document carries content: %+v", document)
	}
}

func requireBoundedExtraction(t *testing.T, body []byte) measuredExtraction {
	t.Helper()
	extraction := readPDFMeasuringAllocation(body)
	requireExtractionInvariants(t, extraction.document, extraction.readable)
	t.Logf("input bytes %d, allocated bytes %d, text bytes %d",
		len(body), extraction.allocatedBytes, len(extraction.document.Text))
	if allocationMeasured && extraction.allocatedBytes > hostileAllocationCeiling {
		t.Fatalf(
			"allocated %d bytes, above the %d byte ceiling",
			extraction.allocatedBytes,
			hostileAllocationCeiling,
		)
	}

	return extraction
}

func skipWhenAllocationIsUnmeasured(t *testing.T) {
	t.Helper()
	if !allocationMeasured {
		t.Skip("this full-size allocation bound runs only in builds that measure allocation")
	}
}

func parallelWhenAllocationIsUnmeasured(t *testing.T) {
	t.Helper()
	if !allocationMeasured {
		t.Parallel()
	}
}

func requireTextContains(t *testing.T, text string, wanted ...string) {
	t.Helper()
	for _, phrase := range wanted {
		if !strings.Contains(text, phrase) {
			t.Fatalf("text misses %q: %q", phrase, text[:min(len(text), 256)])
		}
	}
}

func requireTextOmits(t *testing.T, text string, refused ...string) {
	t.Helper()
	for _, phrase := range refused {
		if strings.Contains(text, phrase) {
			t.Fatalf("text carries isolated %q: %q", phrase, text[:min(len(text), 256)])
		}
	}
}

func contentsArray(references ...int) string {
	var array strings.Builder
	array.WriteString("[")
	for _, reference := range references {
		fmt.Fprintf(&array, "%d 0 R ", reference)
	}
	array.WriteString("]")

	return array.String()
}
