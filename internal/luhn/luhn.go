package luhn

// Validate implements the Luhn algorithm to validate a credit card number.
// It expects a string containing only digits.
func Validate(cardNumber string) bool {
	if len(cardNumber) == 0 {
		return false
	}

	var sum int
	alternate := false
	for i := len(cardNumber) - 1; i >= 0; i-- {
		digit := int(cardNumber[i] - '0')

		if alternate {
			digit *= 2
			if digit > 9 {
				digit = (digit % 10) + (digit / 10)
			}
		}
		sum += digit
		alternate = !alternate
	}

	return sum%10 == 0
}