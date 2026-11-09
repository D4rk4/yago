package yagopdf

import "math"

func (p *pdfTextInterpreter) show(raw []byte, boundary bool) {
	if len(raw) == 0 {
		return
	}
	position := p.position()
	if p.shown && boundary {
		p.separate(position)
	}
	before := p.out.remaining
	pdfWriteShownText(p.out, raw, p.state.font)
	advance, known := p.advance(raw)
	p.state.matrix = p.state.matrix.translated(advance, 0)
	p.previous = p.position()
	p.knownAdvance = known
	p.shown = p.shown || p.out.remaining < before
}

func (p *pdfTextInterpreter) position() pdfMatrix {
	return p.state.matrix.translated(0, p.state.rise).multiply(p.state.transformation)
}

func (p *pdfTextInterpreter) separate(position pdfMatrix) {
	horizontal, vertical, invertible := position.linearInverse(
		position[4]-p.previous[4], position[5]-p.previous[5],
	)
	if !invertible {
		return
	}
	fontSize := math.Abs(p.state.fontSize)
	if math.Abs(vertical) > fontSize*0.5 || horizontal < -fontSize*0.2 {
		p.out.writeByte('\n')
	} else if horizontal > fontSize*math.Abs(p.state.horizontal)*0.102 || !p.knownAdvance {
		p.space()
	}
}

func (p *pdfTextInterpreter) space() {
	text := p.out.String()
	if len(text) > 0 && !pdfWhiteSpace(text[len(text)-1]) {
		p.out.writeByte(' ')
	}
}

func (p *pdfTextInterpreter) advance(raw []byte) (float64, bool) {
	step := 1
	var widths *pdfGlyphWidths
	if p.state.font != nil {
		step = max(1, p.state.font.codeLen)
		widths = p.state.font.widths
	}
	advance, known := 0.0, true
	for at := 0; at < len(raw); at += step {
		code := uint32(0)
		for _, value := range raw[at:min(at+step, len(raw))] {
			code = code<<8 | uint32(value)
		}
		width, exists := widths.width(code)
		known = known && exists
		advance += width*p.state.fontSize/1000 + p.state.characterGap
		if step == 1 && code == 32 {
			advance += p.state.wordGap
		}
	}
	return advance * p.state.horizontal, known
}
