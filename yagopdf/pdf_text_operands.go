package yagopdf

func (s *pdfContentScanner) arrayOperand() pdfContentToken {
	start, depth := s.at, 1
	for {
		end := s.at
		token, ok := s.next(0)
		if !ok {
			return pdfContentToken{kind: pdfContentDictionary}
		}
		switch token.kind {
		case pdfContentArrayStart:
			depth++
		case pdfContentArrayEnd:
			depth--
			if depth == 0 {
				return pdfContentToken{kind: pdfContentArray, value: s.content[start:end]}
			}
		}
	}
}

func (p *pdfTextInterpreter) textOperator(operator string, operands []pdfContentToken) {
	switch operator {
	case "Tf":
		p.selectFont(operands)
	case "Tj", "'", "\"":
		p.showOperand(operator, operands)
	case "TJ":
		if len(operands) == 1 && operands[0].kind == pdfContentArray {
			p.showArray(operands[0].value)
		}
	default:
		p.positionOperator(operator, operands)
	}
}

func (p *pdfTextInterpreter) selectFont(operands []pdfContentToken) {
	if len(operands) != 2 || operands[0].kind != pdfContentName {
		return
	}
	numbers, ok := pdfTextNumbers(operands[1:], 1)
	if !ok {
		return
	}
	p.state.fontSize = numbers[0]
	p.state.font = p.tables[operands[0].name]
	if p.state.font == nil {
		p.state.font = pdfUnavailableFontTable()
	}
}

func (p *pdfTextInterpreter) showOperand(operator string, operands []pdfContentToken) {
	length := 1
	if operator == "\"" {
		length = 3
	}
	if len(operands) < length {
		return
	}
	operands = operands[len(operands)-length:]
	if operands[length-1].kind != pdfContentString {
		return
	}
	if operator == "\"" {
		numbers, ok := pdfTextNumbers(operands[:2], 2)
		if !ok {
			return
		}
		p.state.wordGap, p.state.characterGap = numbers[0], numbers[1]
	}
	if operator != "Tj" {
		p.nextLine()
	}
	p.show(operands[length-1].value, true)
}

func (p *pdfTextInterpreter) showArray(content []byte) {
	scanner := pdfContentScanner{content: content}
	first := true
	for !p.out.complete() {
		token, ok := scanner.next(p.out.rawOperandLimit())
		if !ok {
			return
		}
		switch token.kind {
		case pdfContentString:
			p.show(token.value, first)
			first = false
		case pdfContentNumber:
			p.state.matrix = p.state.matrix.translated(
				-token.number*p.state.fontSize*p.state.horizontal/1000, 0,
			)
			if token.number <= -180 {
				p.space()
			}
		default:
			return
		}
	}
}

func (p *pdfTextInterpreter) positionOperator(operator string, operands []pdfContentToken) {
	length := 1
	switch operator {
	case "Tm":
		length = 6
	case "Td", "TD":
		length = 2
	case "T*":
		length = 0
	}
	numbers, ok := pdfTextNumbers(operands, length)
	if !ok {
		return
	}
	switch operator {
	case "Tm":
		p.state.matrix, p.state.line = pdfMatrix(numbers), pdfMatrix(numbers)
	case "Td", "TD":
		if operator == "TD" {
			p.state.leading = -numbers[1]
		}
		p.state.line = p.state.line.translated(numbers[0], numbers[1])
		p.state.matrix = p.state.line
	case "T*":
		p.nextLine()
	case "TL":
		p.state.leading = numbers[0]
	case "Tc":
		p.state.characterGap = numbers[0]
	case "Tw":
		p.state.wordGap = numbers[0]
	case "Tz":
		p.state.horizontal = numbers[0] / 100
	case "Ts":
		p.state.rise = numbers[0]
	}
}

func (p *pdfTextInterpreter) nextLine() {
	p.state.line = p.state.line.translated(0, -p.state.leading)
	p.state.matrix = p.state.line
	if p.shown {
		p.out.writeByte('\n')
		p.shown = false
	}
}
