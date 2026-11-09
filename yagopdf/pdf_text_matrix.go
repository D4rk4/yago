package yagopdf

type pdfMatrix [6]float64

var pdfIdentityMatrix = pdfMatrix{1, 0, 0, 1, 0, 0}

func (m pdfMatrix) multiply(other pdfMatrix) pdfMatrix {
	return pdfMatrix{
		m[0]*other[0] + m[1]*other[2],
		m[0]*other[1] + m[1]*other[3],
		m[2]*other[0] + m[3]*other[2],
		m[2]*other[1] + m[3]*other[3],
		m[4]*other[0] + m[5]*other[2] + other[4],
		m[4]*other[1] + m[5]*other[3] + other[5],
	}
}

func (m pdfMatrix) translated(horizontal float64, vertical float64) pdfMatrix {
	return pdfMatrix{1, 0, 0, 1, horizontal, vertical}.multiply(m)
}

func (m pdfMatrix) linearInverse(horizontal float64, vertical float64) (float64, float64, bool) {
	determinant := m[0]*m[3] - m[1]*m[2]
	if determinant == 0 {
		return 0, 0, false
	}

	return (m[3]*horizontal - m[2]*vertical) / determinant,
		(m[0]*vertical - m[1]*horizontal) / determinant,
		true
}
