package form

import (
	"net/mail"
	"strings"

	"golang.org/x/net/idna"
)

// FieldErrors contains plain-text messages keyed by form field name.
// A nil or empty map means no field failed validation.
type FieldErrors map[string][]string

const (
	// RequiredMessage describes a missing field value.
	RequiredMessage = "This field is required."
	// EmailMessage describes an invalid email address.
	EmailMessage = "Enter a valid email address."
)

// ValidEmail accepts a single bare address, including quoted local parts,
// single-label domains, and IP literals. It rejects display names, comments,
// surrounding whitespace, control characters, and malformed domain labels.
// It does not normalize the address or check whether it can receive mail.
func ValidEmail(value string) bool {
	if strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 }) {
		return false
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" {
		return false
	}

	separator := strings.LastIndexByte(value, '@')
	if separator < 1 {
		return false
	}
	local, domain := value[:separator], value[separator+1:]
	// net/mail removes quotes and quoted-pair escapes from the local part.
	// It validates their syntax. Keep the original spelling for account lookup.
	quoted := strings.HasPrefix(local, `"`) && strings.HasSuffix(local, `"`)
	if !quoted && address.Address != value {
		return false
	}
	parsedDomain := address.Address[strings.LastIndexByte(address.Address, '@')+1:]
	if domain != parsedDomain {
		return false
	}
	// net/mail already validates IP literals, including the IPv6: prefix.
	if strings.HasPrefix(domain, "[") {
		return true
	}
	return validEmailDomain(domain)
}

func validEmailDomain(domain string) bool {
	// DNS limits apply after IDNA encoding, not to the submitted UTF-8 bytes.
	// Use the encoded domain only for validation. Account lookup keeps the input.
	profile := idna.New(
		idna.MapForLookup(), idna.Transitional(false), idna.BidiRule(),
		idna.VerifyDNSLength(true), idna.CheckHyphens(false),
	)
	encoded, err := profile.ToASCII(domain)
	if err != nil {
		return false
	}

	// Ordinary ASCII labels can contain internal double hyphens. Internationalized
	// labels must also satisfy IDNA's stricter rules after decoding their Punycode.
	for _, label := range strings.Split(encoded, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		if strings.HasPrefix(label, "xn--") {
			if _, err := idna.New(idna.ValidateForRegistration()).ToASCII(label); err != nil {
				return false
			}
		}
	}
	return true
}
