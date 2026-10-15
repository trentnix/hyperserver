// Package password hashes passwords with bcrypt and checks password complexity.
package password

import (
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	minimumPasswordLength = 8
	maximumPasswordBytes  = 72
	passwordRequirements  = "password must contain at least 8 characters (Unicode code points), an ASCII letter, a digit, and one of @$!%*?&"
)

// HashPassword hashes password with bcrypt's default cost.
// It rejects passwords over 72 bytes but does not check password-complexity rules.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash compares a password of at most 72 bytes with its bcrypt hash.
// It does not apply new-password complexity rules to existing credentials.
func CheckPasswordHash(password, hash string) bool {
	if len(password) > maximumPasswordBytes {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ValidatePasswordComplexity requires valid UTF-8, at least eight Unicode code
// points, at most 72 bytes, an ASCII letter, an ASCII digit, and one of @$!%*?&.
// Combining marks count separately. It does not trim, normalize, hash, or store
// the password. Call before hashing new or replacement passwords.
func ValidatePasswordComplexity(password string) error {
	if len(password) > maximumPasswordBytes {
		return errors.New("password must not exceed 72 bytes in UTF-8")
	}
	if !utf8.ValidString(password) {
		return errors.New("password must contain valid UTF-8 text")
	}

	length := 0
	var hasLetter, hasDigit, hasSpecialChar bool
	for _, character := range password {
		length++
		switch {
		case character >= 'A' && character <= 'Z', character >= 'a' && character <= 'z':
			hasLetter = true
		case character >= '0' && character <= '9':
			hasDigit = true
		case strings.ContainsRune("@$!%*?&", character):
			hasSpecialChar = true
		}
	}
	if length < minimumPasswordLength || !hasLetter || !hasDigit || !hasSpecialChar {
		return errors.New(passwordRequirements)
	}
	return nil
}
