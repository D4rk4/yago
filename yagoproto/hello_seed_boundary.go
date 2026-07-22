package yagoproto

const HelloSeedMaximumUTF16Units = 16000

func helloSeedWithinWireBoundary(value string) bool {
	units := 0
	for _, character := range value {
		units++
		if character > '\uffff' {
			units++
		}
		if units > HelloSeedMaximumUTF16Units {
			return false
		}
	}

	return true
}
