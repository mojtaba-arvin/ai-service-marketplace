package marketplace

import (
	"errors"
	"testing"
)

func TestNormalizeE164Phone(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "canonical", input: "+14155552671", want: "+14155552671"},
		{name: "spaces and punctuation", input: " +1 (415) 555-2671 ", want: "+14155552671"},
		{name: "international short code", input: "+968 9123 4567", want: "+96891234567"},
		{name: "maximum E164 length", input: "+123456789012345", want: "+123456789012345"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeE164Phone(test.input)
			if err != nil {
				t.Fatalf("NormalizeE164Phone() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizeE164Phone() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeE164PhoneRejectsInvalidInput(t *testing.T) {
	inputs := []string{
		"", "4155552671", "++14155552671", "+", "+0", "+0123",
		"+1", "+1234567890123456", "+1-800-FLOWERS", "+1 ext 2",
		"+١٢٣٤", "+1/415/555/2671",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			if got, err := NormalizeE164Phone(input); !errors.Is(err, ErrInvalidPhone) {
				t.Fatalf("NormalizeE164Phone(%q) = %q, %v; want ErrInvalidPhone", input, got, err)
			}
		})
	}
}
