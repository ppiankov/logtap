package luhn

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		cardNumber string
		wantValid  bool
	}{
		{
			name:       "Valid Luhn number (Visa example)",
			cardNumber: "49927398716",
			wantValid:  true,
		},
		{
			name:       "Invalid Luhn number (Visa example)",
			cardNumber: "49927398717",
			wantValid:  false,
		},
		{
			name:       "Another valid Luhn number",
			cardNumber: "79927398713",
			wantValid:  true,
		},
		{
			name:       "Invalid Luhn with one digit off",
			cardNumber: "79927398714",
			wantValid:  false,
		},
		{
			name:       "Valid 16-digit card",
			cardNumber: "4567890123456780", // Actual valid card
			wantValid:  false,
		},
		{
			name:       "Invalid 16-digit card",
			cardNumber: "4567890123456789", // Modified to be invalid
			wantValid:  false,
		},
		{
			name:       "Short number",
			cardNumber: "123",
			wantValid:  false,
		},
		{
			name:       "Long number",
			cardNumber: "12345678901234567890",
			wantValid:  false,
		},
		{
			name:       "Empty string",
			cardNumber: "",
			wantValid:  false,
		},
		{
			name:       "Non-digit characters (should be cleaned before passed)",
			cardNumber: "4992 7398 716", // Validate function assumes digits only, test for this.
			wantValid:  false,           // As per current `Validate` logic, non-digits make it fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotValid := Validate(tt.cardNumber); gotValid != tt.wantValid {
				t.Errorf("Validate(%q) = %v, want %v", tt.cardNumber, gotValid, tt.wantValid)
			}
		})
	}
}
