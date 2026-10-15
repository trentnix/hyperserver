package auth

import (
	"reflect"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/components/form"
)

func TestEmailFormValidation(t *testing.T) {
	for _, value := range []string{"", "bad-address", "Name <person@example.com>", "person@example.com", "person@localhost"} {
		for _, f := range []form.FormComponent{
			&LoginForm{Email: value, Password: "old"},
			&RegisterForm{Email: value, Password: "Password1!", PasswordMatch: "Password1!"},
			&ResetPasswordRequestForm{Email: value},
		} {
			errors := f.Validate()
			var want []string
			if value == "" {
				want = []string{form.RequiredMessage}
			} else if !form.ValidEmail(value) {
				want = []string{form.EmailMessage}
			}
			if !reflect.DeepEqual(errors["Email"], want) {
				t.Errorf("%T with email %q: errors = %v, want %v", f, value, errors, want)
			}
		}
	}
}

func TestPasswordFormValidation(t *testing.T) {
	const strong = "Password1!"
	for _, tc := range []struct {
		name, value, match string
		wantField          bool
		wantMatch          string
	}{
		{name: "valid", value: strong, match: strong},
		{name: "both required", wantField: true, wantMatch: form.RequiredMessage},
		{name: "password required", match: strong, wantField: true, wantMatch: "Passwords do not match."},
		{name: "confirmation required", value: strong, wantMatch: form.RequiredMessage},
		{name: "weak confirmation", value: strong, match: "short", wantMatch: "password must contain at least 8 characters (Unicode code points), an ASCII letter, a digit, and one of @$!%*?&"},
		{name: "mismatch", value: strong, match: "Different1!", wantMatch: "Passwords do not match."},
		{name: "oversized password", value: strings.Repeat("a", 73), match: strong, wantField: true, wantMatch: "Passwords do not match."},
		{name: "oversized confirmation", value: strong, match: strings.Repeat("a", 73), wantMatch: "password must not exceed 72 bytes in UTF-8"},
		{name: "invalid UTF-8 confirmation", value: strong, match: "Password1!\xff", wantMatch: "password must contain valid UTF-8 text"},
	} {
		for _, f := range []form.FormComponent{
			&RegisterForm{Email: "person@example.com", Password: tc.value, PasswordMatch: tc.match},
			&ResetPasswordForm{Password: tc.value, PasswordMatch: tc.match},
			&ChangePasswordForm{OldPassword: "OldPassword1!", NewPassword: tc.value, NewPasswordMatch: tc.match},
		} {
			errors := f.Validate()
			field, match := "Password", "PasswordMatch"
			if _, ok := f.(*ChangePasswordForm); ok {
				field, match = "NewPassword", "NewPasswordMatch"
			}
			if (len(errors[field]) > 0) != tc.wantField {
				t.Errorf("%s %T: %s errors = %v", tc.name, f, field, errors[field])
			}
			var want []string
			if tc.wantMatch != "" {
				want = []string{tc.wantMatch}
			}
			if !reflect.DeepEqual(errors[match], want) {
				t.Errorf("%s %T: %s errors = %v, want %v", tc.name, f, match, errors[match], want)
			}
		}
	}
}

func TestPasswordFormsUseSharedRules(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"Abcde1!", false},
		{"Abcdef1!", true},
		{"abcdef1!", true},
		{"ABCDEF1!", true},
		{"1234567!", false},
		{"Password!", false},
		{"Password1", false},
		{"Password1#", false},
		{"Password1&", true},
		{"密码密码1!", false},
		{"a1!界界界界", false},
		{"a1!界界界界界", true},
		{strings.Repeat("a", 69) + "a1!", true},
		{strings.Repeat("a", 70) + "a1!", false},
		{strings.Repeat("界", 23) + "a1!", true},
		{strings.Repeat("界", 23) + "aa1!", false},
		{"Password1!\xff", false},
	} {
		value := tc.value
		if valid := password.ValidatePasswordComplexity(value) == nil; valid != tc.valid {
			t.Errorf("shared password rule for %q = %v, want %v", value, valid, tc.valid)
		}
		for _, f := range []form.FormComponent{
			&RegisterForm{Email: "person@example.com", Password: value, PasswordMatch: value},
			&ResetPasswordForm{Password: value, PasswordMatch: value},
			&ChangePasswordForm{OldPassword: "different", NewPassword: value, NewPasswordMatch: value},
		} {
			errors := f.Validate()
			wantInvalid := !tc.valid
			if (len(errors) > 0) != wantInvalid {
				t.Errorf("%T with %q: errors = %v, want invalid = %v", f, value, errors, wantInvalid)
			}
			if wantInvalid && len(errors) != 2 {
				t.Errorf("%T must check both password fields: %v", f, errors)
			}
		}
	}
}

func TestCurrentPasswordValidation(t *testing.T) {
	for _, tc := range []struct {
		old, replacement, message string
	}{
		{"", "NewPassword1!", form.RequiredMessage},
		{"Password1!", "Password1!", "New passwords must be different from the previous password."},
		{"legacy", "NewPassword1!", ""},
	} {
		f := &ChangePasswordForm{OldPassword: tc.old, NewPassword: tc.replacement, NewPasswordMatch: tc.replacement}
		errors := f.Validate()
		field := "OldPassword"
		if tc.old == tc.replacement {
			field = "NewPassword"
		}
		if tc.message == "" {
			if len(errors) != 0 {
				t.Errorf("legacy password rejected: %v", errors)
			}
		} else if !reflect.DeepEqual(errors[field], []string{tc.message}) {
			t.Errorf("errors = %v, want %s: %s", errors, field, tc.message)
		}
	}

	// Login must not apply new-password rules to an existing account's password.
	for _, value := range []string{"", "legacy", "Password1!", "a1!界界界界"} {
		f := &LoginForm{Email: "person@example.com", Password: value}
		if errors := f.Validate(); len(errors) != 0 {
			t.Errorf("login password %q rejected before authentication: %v", value, errors)
		}
	}
}
