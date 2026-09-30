// Package password hashes passwords with bcrypt and checks password complexity.
package password

import (
	"fmt"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

const (
	minimumPasswordLength = 8
)

// HashPassword hashes password with bcrypt's default cost.
// It does not check the application's password-complexity rules.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash checks the provided password to see if it matches the specified hash value
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ValidatePasswordComplexity requires at least eight bytes, an ASCII letter,
// a digit, and one of @$!%*?&. It does not hash or store the password.
func ValidatePasswordComplexity(password string) error {
	if len(password) < minimumPasswordLength {
		return fmt.Errorf("a password requires at least 8 characters, one letter, one number, and one special character")
	}

	// Check for at least one letter
	hasLetter := regexp.MustCompile(`[A-Za-z]`).MatchString(password)

	// Check for at least one digit
	hasDigit := regexp.MustCompile(`\d`).MatchString(password)

	// Check for at least one special character
	hasSpecialChar := regexp.MustCompile(`[@$!%*?&]`).MatchString(password)

	// Return true only if all conditions are met
	if hasLetter && hasDigit && hasSpecialChar {
		return nil
	}

	return fmt.Errorf("a password requires at least 8 characters, one letter, one number, and one special character")
}
