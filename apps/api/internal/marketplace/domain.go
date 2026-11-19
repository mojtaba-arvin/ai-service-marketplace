package marketplace

import (
	"errors"
	"strings"
)

var (
	ErrInvalidAmount = errors.New("amount_minor must be positive")
	ErrInvalidCurrency = errors.New("currency must be a three-letter uppercase code")
	ErrInvalidTransition = errors.New("invalid order state transition")
)

func ValidateAmount(amount int64, currency string) error {
	if amount <= 0 { return ErrInvalidAmount }
	if len(currency) != 3 || strings.ToUpper(currency) != currency {
		return ErrInvalidCurrency
	}
	for _, r := range currency { if r < 'A' || r > 'Z' { return ErrInvalidCurrency } }
	return nil
}

func CanTransition(from, to string) bool {
	switch from {
	case "requested":
		return to == "accepted" || to == "cancelled"
	case "accepted":
		return to == "in_progress" || to == "cancelled"
	case "in_progress":
		return to == "completed" || to == "disputed"
	default:
		return false
	}
}
