package yagopdf

import "math"

const pdfMaxGraphicsDepth = 64

type pdfTextState struct {
	font           *pdfCMap
	fontSize       float64
	characterGap   float64
	wordGap        float64
	horizontal     float64
	leading        float64
	rise           float64
	matrix         pdfMatrix
	line           pdfMatrix
	transformation pdfMatrix
}

type pdfTextInterpreter struct {
	out          *pdfTextCollector
	tables       map[string]*pdfCMap
	state        pdfTextState
	saved        []pdfTextState
	overflow     int
	inside       bool
	previous     pdfMatrix
	shown        bool
	knownAdvance bool
}

func newPDFTextInterpreter(out *pdfTextCollector, tables map[string]*pdfCMap) *pdfTextInterpreter {
	return &pdfTextInterpreter{
		out:    out,
		tables: tables,
		state: pdfTextState{
			fontSize:       1,
			horizontal:     1,
			matrix:         pdfIdentityMatrix,
			line:           pdfIdentityMatrix,
			transformation: pdfIdentityMatrix,
		},
	}
}

func pdfWriteContentText(out *pdfTextCollector, content []byte, tables map[string]*pdfCMap) {
	newPDFTextInterpreter(out, tables).read(content)
}

func (p *pdfTextInterpreter) read(content []byte) {
	scanner := pdfContentScanner{content: content}
	operands := make([]pdfContentToken, 0, 6)
	overflowed, retained := false, 0
	for !p.out.complete() {
		token, ok := scanner.next(max(0, p.out.rawOperandLimit()-retained))
		if !ok {
			return
		}
		if token.kind == pdfContentArrayStart {
			token = scanner.arrayOperand()
		}
		if token.kind != pdfContentOperator {
			if len(operands) == cap(operands) {
				overflowed = true
				continue
			}
			operands = append(operands, token)
			retained += len(token.value)
			continue
		}
		if !overflowed {
			p.operator(string(token.value), operands)
		}
		operands, overflowed, retained = operands[:0], false, 0
	}
}

func (p *pdfTextInterpreter) operator(operator string, operands []pdfContentToken) {
	switch operator {
	case "BT":
		p.inside = true
		p.shown = false
		p.state.matrix, p.state.line = pdfIdentityMatrix, pdfIdentityMatrix
	case "ET":
		if p.inside && p.shown {
			p.out.writeByte('\n')
		}
		p.inside = false
	case "q":
		if len(p.saved) < pdfMaxGraphicsDepth && p.overflow == 0 {
			p.saved = append(p.saved, p.state)
		} else {
			p.overflow++
		}
	case "Q":
		p.restore()
	case "cm":
		if numbers, ok := pdfTextNumbers(operands, 6); ok {
			p.state.transformation = pdfMatrix(numbers).multiply(p.state.transformation)
		}
	case "Tf", "Tc", "Tw", "Tz", "TL", "Ts":
		p.textOperator(operator, operands)
	default:
		if p.inside {
			p.textOperator(operator, operands)
		}
	}
}

func (p *pdfTextInterpreter) restore() {
	if p.overflow > 0 {
		p.overflow--
		return
	}
	if len(p.saved) > 0 {
		matrix, line := p.state.matrix, p.state.line
		p.state = p.saved[len(p.saved)-1]
		p.saved = p.saved[:len(p.saved)-1]
		p.state.matrix, p.state.line = matrix, line
	}
}

func pdfTextNumbers(operands []pdfContentToken, length int) ([6]float64, bool) {
	var numbers [6]float64
	if len(operands) != length {
		return numbers, false
	}
	for index, operand := range operands {
		if operand.kind != pdfContentNumber || math.IsInf(operand.number, 0) ||
			math.IsNaN(operand.number) {
			return numbers, false
		}
		numbers[index] = operand.number
	}
	return numbers, true
}
