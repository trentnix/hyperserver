package password

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestValidatePasswordComplexity(t *testing.T) {
	for _, tc := range []struct {
		name, value, message string
	}{
		{"empty", "", passwordRequirements},
		{"seven ASCII characters", "Abcde1!", passwordRequirements},
		{"eight ASCII characters", "Abcdef1!", ""},
		{"lowercase letter", "abcdef1!", ""},
		{"uppercase letter", "ABCDEF1!", ""},
		{"no letter", "1234567!", passwordRequirements},
		{"no digit", "Password!", passwordRequirements},
		{"no special character", "Password1", passwordRequirements},
		{"unsupported special character", "Password1#", passwordRequirements},
		{"non-ASCII letter only", "密码密码密码1!", passwordRequirements},
		{"non-ASCII digit only", "Password١!", passwordRequirements},
		{"seven Unicode characters", "a1!界界界界", passwordRequirements},
		{"eight Unicode characters", "a1!界界界界界", ""},
		{"combining marks count separately", "a1!e\u0301e\u0301x", ""},
		{"composed characters count once", "a1!ééx", passwordRequirements},
		{"spaces are significant", " a1!    ", ""},
		{"72 ASCII bytes", strings.Repeat("a", 69) + "a1!", ""},
		{"73 ASCII bytes", strings.Repeat("a", 70) + "a1!", "password must not exceed 72 bytes in UTF-8"},
		{"72 Unicode bytes", strings.Repeat("界", 23) + "a1!", ""},
		{"73 Unicode bytes", strings.Repeat("界", 23) + "aa1!", "password must not exceed 72 bytes in UTF-8"},
		{"invalid UTF-8", "Password1!\xff", "password must contain valid UTF-8 text"},
		{"replacement character", "Password1!\uFFFD", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePasswordComplexity(tc.value)
			if tc.message == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.message {
				t.Fatalf("error=%v, want %q", err, tc.message)
			}
		})
	}
	for _, special := range "@$!%*?&" {
		if err := ValidatePasswordComplexity("Password1" + string(special)); err != nil {
			t.Errorf("supported special character %q: %v", special, err)
		}
	}
}

func TestPasswordHashByteLimit(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 69) + "a1!", strings.Repeat("界", 23) + "a1!"} {
		hash, err := HashPassword(value)
		if err != nil {
			t.Fatal(err)
		}
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil || cost != bcrypt.DefaultCost {
			t.Fatalf("cost=%d, error=%v", cost, err)
		}
		if !CheckPasswordHash(value, hash) {
			t.Fatal("72-byte password did not match its hash")
		}
		if CheckPasswordHash(value+"x", hash) {
			t.Fatal("accepted a longer password sharing the same first 72 bytes")
		}
		if encoded, err := HashPassword(value + "x"); encoded != "" || !errors.Is(err, bcrypt.ErrPasswordTooLong) {
			t.Fatalf("73-byte hash=%q, error=%v", encoded, err)
		}
	}
}

func TestPasswordComparisonPreservesExistingCredentials(t *testing.T) {
	for _, tc := range []struct{ value, different string }{
		{"old", "Old"},
		{" Password1! ", "Password1!"},
		{"Password1!é", "Password1!e\u0301"},
	} {
		hash, err := HashPassword(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if !CheckPasswordHash(tc.value, hash) || CheckPasswordHash(tc.different, hash) {
			t.Fatal("comparison changed password bytes or enforced creation rules")
		}
	}
	if CheckPasswordHash("Password1!", "invalid-hash") {
		t.Fatal("accepted an invalid hash")
	}
}
