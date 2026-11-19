package marketplace

import (
	"errors"
	"strings"
)

var ErrInvalidPhone = errors.New("phone number must be a valid E.164 number")

// NormalizeE164Phone canonicalizes common visual separators while requiring an
// explicit international country calling code. It validates E.164 syntax;
// country-specific numbering-plan validation belongs in a dedicated adapter.
func NormalizeE164Phone(input string) (string, error) {
	value := strings.TrimSpace(input)
	if len(value) == 0 || value[0] != '+' {
		return "", ErrInvalidPhone
	}

	var digits strings.Builder
	for i := 1; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch >= '0' && ch <= '9':
			digits.WriteByte(ch)
		case ch == ' ' || ch == '-' || ch == '(' || ch == ')' || ch == '.':
			// Formatting separators are intentionally discarded.
		default:
			return "", ErrInvalidPhone
		}
	}

	number := digits.String()
	if len(number) < 2 || len(number) > 15 || number[0] == '0' {
		return "", ErrInvalidPhone
	}
	return "+" + number, nil
}
