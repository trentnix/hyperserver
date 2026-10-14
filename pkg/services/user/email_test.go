package user

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEmailKey(t *testing.T) {
	for _, tc := range []struct {
		first, second string
		equal         bool
	}{
		{"Trent@Example.com", "trent@example.COM", true},
		{"É@BÜCHER.example", "é@bücher.EXAMPLE", true},
		{"σ@example.com", "ς@EXAMPLE.COM", true},
		{"s@example.com", "ſ@example.com", true},
		{"i@example.com", "İ@example.com", false},
		{"user+tag@example.com", "user@example.com", false},
		{"first.last@example.com", "firstlast@example.com", false},
		{" user@example.com", "user@example.com", false},
	} {
		if got := EmailKey(tc.first) == EmailKey(tc.second); got != tc.equal {
			t.Errorf("EmailKey comparison of %q and %q = %t", tc.first, tc.second, got)
		}
	}
}

func FuzzEmailKey(f *testing.F) {
	f.Add("Trent@Example.com", "trent@example.COM")
	f.Add("σ@example.com", "ς@example.com")
	f.Fuzz(func(t *testing.T, first, second string) {
		if !utf8.ValidString(first) || !utf8.ValidString(second) {
			t.Skip("email addresses must be valid UTF-8")
		}
		if (EmailKey(first) == EmailKey(second)) != strings.EqualFold(first, second) {
			t.Fatal("comparison keys disagree with Unicode simple folding")
		}
		if EmailKey(EmailKey(first)) != EmailKey(first) {
			t.Fatal("comparison key is not stable")
		}
	})
}
