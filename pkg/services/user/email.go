package user

import (
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// EmailKey returns an account lookup and uniqueness key. It compares local parts
// with Unicode simple folding and DNS domains in their IDNA ASCII form.
// It does not validate addresses, trim whitespace, remove dots or tags, or change
// local-part quoting or Unicode normalization. Keep the original email for delivery.
func EmailKey(email string) string {
	separator := strings.LastIndexByte(email, '@')
	if separator >= 0 {
		local, domain := email[:separator], email[separator+1:]
		// Match the non-transitional IDNA policy used by form validation. Ordinary
		// ASCII labels may contain internal double hyphens.
		profile := idna.New(idna.MapForLookup(), idna.Transitional(false),
			idna.BidiRule(), idna.VerifyDNSLength(true), idna.CheckHyphens(false))
		if encoded, err := profile.ToASCII(domain); err == nil {
			// Keep ASCII keys in their existing uppercase form. Domain conversion
			// must precede folding: Unicode case equivalence is not IDNA equivalence.
			return foldEmailLocal(local) + "@" + strings.ToUpper(encoded)
		}
	}
	// Preserve deterministic keys for address literals and invalid input. Callers
	// must validate submitted addresses before using them for account operations.
	return foldEmailLocal(email)
}

func foldEmailLocal(value string) string {
	return strings.Map(func(r rune) rune {
		// Choose the same representative for every rune in a case-folding cycle.
		key := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			if folded < key {
				key = folded
			}
		}
		return key
	}, value)
}
