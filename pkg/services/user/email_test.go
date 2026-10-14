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
		{"É@BÜCHER.example", "é@xn--bcher-kva.example", true},
		{"é@bu\u0308cher.example", "É@XN--BCHER-KVA.EXAMPLE", true},
		{"用户@例え.テスト", "用户@xn--r8jz45g.xn--zckzah", true},
		{`"first@last"@bücher.example`, `"FIRST@LAST"@xn--bcher-kva.example`, true},
		{"person@ab--cd.bücher.example", "PERSON@ab--cd.xn--bcher-kva.example", true},
		{"person@faß.example", "person@fass.example", false},
		{"person@σ.example", "person@ς.example", false},
		{"person@example.com", "person@еxample.com", false}, // Cyrillic е is not Latin e.
		{"é@example.com", "e\u0301@example.com", false},
		{"person@[IPv6:::1]", "PERSON@[ipv6:::1]", true},
		{"person@-bücher.example", "person@xn--bcher-kva.example", false},
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
	f.Add("É@bücher.example", "é@xn--bcher-kva.example")
	f.Add("person@faß.example", "person@fass.example")
	f.Fuzz(func(t *testing.T, first, second string) {
		if !utf8.ValidString(first) || !utf8.ValidString(second) {
			t.Skip("email addresses must be valid UTF-8")
		}
		if (foldEmailLocal(first) == foldEmailLocal(second)) != strings.EqualFold(first, second) {
			t.Fatal("local-part comparison disagrees with Unicode simple folding")
		}
		if EmailKey(EmailKey(first)) != EmailKey(first) {
			t.Fatal("comparison key is not stable")
		}
	})
}

func TestEmailKeyPreservesASCIIKeys(t *testing.T) {
	for _, email := range []string{"Person.Name+tag@Example.COM", `"First Last"@example.com`, "person@ab--cd.example"} {
		if got, want := EmailKey(email), foldEmailLocal(email); got != want {
			t.Errorf("ASCII key changed: %q, want %q", got, want)
		}
	}
}
