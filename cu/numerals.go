package cu

import (
	"fmt"
	"strconv"
)

// Cyrillic numeral system characters.
// The system uses letters to represent numbers, with special markers for thousands.
var (
	// Units: 1-9
	cyrillicUnits = []rune{0, 'а', 'в', 'г', 'д', 'є', 'ѕ', 'з', 'и', 'ѳ'}
	// Tens: 10-90 (70 is ѻ, as the printed books write it)
	cyrillicTens = []rune{0, 'і', 'к', 'л', 'м', 'н', 'ѯ', 'ѻ', 'п', 'ч'}
	// Hundreds: 100-900
	cyrillicHundreds = []rune{0, 'р', 'с', 'т', 'у', 'ф', 'х', 'ѱ', 'ѡ', 'ц'}
)

const (
	titlo           = '\u0483' // ҃ combining titlo
	thousandsMarker = '҂'      // thousands prefix
)

// ToCyrillicNumeral converts a number to Cyrillic numeral notation.
// Supports numbers from 1 to 999,999 (and can be extended further).
// Examples:
//   - 1 → "а҃"
//   - 15 → "є҃і" (units before tens for 11-19)
//   - 100 → "р҃"
//   - 1564 → "҂аф҃ѯд" (1000 + 500 + 60 + 4)
//   - 7071 → "҂зѻ҃а" (7000 + 70 + 1)
func ToCyrillicNumeral(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}

	var parts []rune

	// Handle thousands (1000-999999)
	// For numbers >= 1000, we prefix with ҂ and the digit(s)
	if n >= 1000 {
		thousands := n / 1000
		n %= 1000

		// Convert the thousands part (without titlo)
		// This handles cases like 10000 = ҂і, 25000 = ҂кє, etc.
		parts = append(parts, thousandsMarker)
		thousandsParts := cyrillicNumeralParts(thousands)
		parts = append(parts, thousandsParts...)
	}

	// Handle remaining hundreds, tens, units
	remaining := cyrillicNumeralParts(n)
	parts = append(parts, remaining...)

	if len(parts) == 0 {
		return "0"
	}

	// Add titlo: for multi-character numerals, it goes before the last character
	// For single character, it goes after
	if len(parts) == 1 || (len(parts) == 2 && parts[0] == thousandsMarker) {
		return string(parts) + string(titlo)
	}

	// Insert titlo before the last character
	result := make([]rune, 0, len(parts)+1)
	result = append(result, parts[:len(parts)-1]...)
	result = append(result, titlo)
	result = append(result, parts[len(parts)-1])
	return string(result)
}

// cyrillicNumeralParts returns the Cyrillic letters for a number 0-999 (without titlo).
func cyrillicNumeralParts(n int) []rune {
	if n <= 0 || n > 999 {
		return nil
	}

	var parts []rune

	// Hundreds
	if n >= 100 {
		parts = append(parts, cyrillicHundreds[n/100])
		n %= 100
	}

	// Special case: 11-19 are written as unit + і (reversed order)
	if n >= 11 && n <= 19 {
		parts = append(parts, cyrillicUnits[n-10])
		parts = append(parts, cyrillicTens[1]) // і = 10
		return parts
	}

	// Tens
	if n >= 10 {
		parts = append(parts, cyrillicTens[n/10])
		n %= 10
	}

	// Units
	if n > 0 {
		parts = append(parts, cyrillicUnits[n])
	}

	return parts
}

// ParseNumeral is the number a numeral writes, Arabic («233») or Cyrillic
// («сл҃г»). A Cyrillic numeral is the sum of its letters' values, so the
// titlo may be anywhere or absent and the order of the letters does not
// matter («а҃і» and «і҃а» are both 11); ҂ makes the letter after it
// thousands. о is 70, as ѻ, and ѿ 800, as ѡ. A letter with no value is
// an error.
func ParseNumeral(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("ParseNumeral: empty numeral")
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	n, scale := 0, 1
	for _, r := range s {
		switch r {
		case titlo:
			continue
		case thousandsMarker:
			scale = 1000
			continue
		}
		v := letterValue(r)
		if v == 0 {
			return 0, fmt.Errorf("ParseNumeral: %q: %q is not a numeral letter", s, r)
		}
		n += v * scale
		scale = 1
	}
	if scale != 1 {
		return 0, fmt.Errorf("ParseNumeral: %q: nothing after %q", s, thousandsMarker)
	}
	return n, nil
}

// letterValue is the value of a numeral letter, 0 for any other rune.
func letterValue(r rune) int {
	switch r {
	case 'о':
		r = 'ѻ'
	case 'ѿ':
		r = 'ѡ'
	}
	for i := 1; i <= 9; i++ {
		switch r {
		case cyrillicUnits[i]:
			return i
		case cyrillicTens[i]:
			return i * 10
		case cyrillicHundreds[i]:
			return i * 100
		}
	}
	return 0
}

// NumeralWriter is the writer of the numerals a document's Numerals key
// names: "cyrillic" («сл҃г») or "arabic" («233»).
func NumeralWriter(style string) (func(int) string, error) {
	switch style {
	case "cyrillic":
		return ToCyrillicNumeral, nil
	case "arabic":
		return strconv.Itoa, nil
	}
	return nil, fmt.Errorf("NumeralWriter: unknown numerals %q (cyrillic, arabic)", style)
}
