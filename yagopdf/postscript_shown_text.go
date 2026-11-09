package yagopdf

func pdfWritePostScriptText(out *pdfTextCollector, body []byte) {
	scanner := pdfContentScanner{content: body[:min(len(body), pdfMaxObjScanBytes)]}
	var shown []byte
	for !out.complete() {
		token, ok := scanner.next(out.rawOperandLimit())
		if !ok {
			return
		}
		switch token.kind {
		case pdfContentString:
			shown = token.value
		case pdfContentOperator:
			switch string(token.value) {
			case "show", "ashow", "widthshow", "awidthshow", "kshow",
				"xshow", "yshow", "xyshow":
				pdfWriteDecodedStringBytes(out, shown)
				if len(shown) > 0 {
					out.writeByte('\n')
				}
			}
			shown = nil
		}
	}
}
