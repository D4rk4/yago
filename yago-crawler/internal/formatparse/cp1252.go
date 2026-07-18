package formatparse

// cp1252High maps the 0x80-0x9F range where Windows-1252 diverges from
// Latin-1; the legacy Office "compressed" 8-bit strings are cp1252. A zero
// entry marks an undefined slot, decoded as the raw byte value.
var cp1252High = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// decodeCP1252 maps one Windows-1252 byte to its rune.
func decodeCP1252(b byte) rune {
	if b >= 0x80 && b <= 0x9F {
		if mapped := cp1252High[b-0x80]; mapped != 0 {
			return mapped
		}
	}

	return rune(b)
}
