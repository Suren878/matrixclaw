package phone

import "strings"

// Normalize keeps only decimal digits and normalizes Russian local 8xxxxxxxxxx
// numbers to 7xxxxxxxxxx for SIP/provider consistency.
func Normalize(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "+")
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	phone := b.String()
	if len(phone) == 11 && phone[0] == '8' {
		return "7" + phone[1:]
	}
	return phone
}
