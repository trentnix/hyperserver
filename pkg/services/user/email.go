package user

import (
	"strings"
	"unicode"
)

// EmailKey returns a case-insensitive account lookup key using Unicode simple
// folding, matching strings.EqualFold. It does not trim whitespace, remove dots
// or tags, or otherwise normalize addresses. Keep the original email for delivery.
func EmailKey(email string) string {
	return strings.Map(func(r rune) rune {
		// Choose the same representative for every rune in a case-folding cycle.
		key := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			if folded < key {
				key = folded
			}
		}
		return key
	}, email)
}
