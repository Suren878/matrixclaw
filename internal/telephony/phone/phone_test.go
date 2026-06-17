package phone

import "testing"

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"":                  "",
		"+1 (555) 010-2200": "15550102200",
		"8 (916) 123-45-67": "79161234567",
		"+7 916 123-45-67":  "79161234567",
		"007-123":           "007123",
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}
