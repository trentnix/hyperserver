package form

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestValidEmail(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"person@example.com", true},
		{"Person.Name+tag@Sub.Example.com", true},
		{"person@localhost", true},
		{"person@xn--bcher-kva.example", true},
		{"person@例え.テスト", true},
		{"person@sub-domain.example", true},
		{`"person"@example.com`, true},
		{`"first last"@example.com`, true},
		{`"first..last"@example.com`, true},
		{`"first@last"@example.com`, true},
		{`"first\"last"@example.com`, true},
		{`"first\\last"@example.com`, true},
		{`"(person)"@example.com`, true},
		{"person@[127.0.0.1]", true},
		{"person@[IPv6:::1]", true},
		{`"person"@[127.0.0.1]`, true},
		{`"person"@[IPv6:::1]`, true},
		{"person@" + strings.Repeat("a", 63) + ".com", true},
		{"person@" + strings.Repeat("a.", 126) + "a", true},
		{"", false},
		{"not-an-email", false},
		{"person@", false},
		{"@example.com", false},
		{"person@@example.com", false},
		{"a..b@example.com", false},
		{"Person <person@example.com>", false},
		{"<person@example.com>", false},
		{"person@example.com (Person)", false},
		{"person@example.com, other@example.com", false},
		{" person@example.com", false},
		{"person@example.com ", false},
		{"person@example.com\r\nBcc: other@example.com", false},
		{"person@\nexample.com", false},
		{"person@foo/bar.com", false},
		{"person@-example.com", false},
		{"person@example-.com", false},
		{"person@foo_bar.com", false},
		{"person@foo..com", false},
		{"person@.example.com", false},
		{"person@\u0301example.com", false},
		{"person@אa.example", false},
		{"person@xn--a.example", false},
		{"person@[999.0.0.1]", false},
		{"person@[::1]", false},
		{"person@[IPv6:invalid]", false},
		{"person@" + strings.Repeat("a", 64) + ".com", false},
		{"person@" + strings.Repeat("a.", 127) + "a", false},
		{`"person"@foo/bar.com`, false},
		{`"person"@-example.com`, false},
		{`"Person" <person@example.com>`, false},
		{`"" <"person"@example.com>`, false},
		{`"person"@example.com (Person)`, false},
		{`"person"@example.com ()`, false},
		{`"person"@[127.0.0.1] ()`, false},
		{`"person"@example.com `, false},
		{` "person"@example.com`, false},
		{`"person" @example.com`, false},
		{`"person"@ example.com`, false},
		{`"person"(comment)@example.com`, false},
		{`"person"@example.com, other@example.com`, false},
		{`"person@example.com`, false},
		{`"person""extra"@example.com`, false},
		{"\"per\tson\"@example.com", false},
		{"\"per\x00son\"@example.com", false},
		{"\"per\x7fson\"@example.com", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got := ValidEmail(tc.value); got != tc.valid {
				t.Errorf("ValidEmail(%q) = %v, want %v", tc.value, got, tc.valid)
			}
		})
	}
}

func TestValidEmailDomainPolicies(t *testing.T) {
	for _, tc := range []struct {
		domain string
		valid  bool
	}{
		{"ab--cd.example", true},
		{"AB--CD.Example", true},
		{"r3---sn.example", true},
		{"sub--domain.example", true},
		{"a--b.example", true},
		{"ab--cd", true},
		{"ab--cd.例え", true},
		{"例え.ab--cd", true},
		{"ab--cd.xn--r8jz45g", true},
		{"xn--r8jz45g.ab--cd", true},
		{"ab--cd。例え", true},
		{"ab--cd.עברית", true},
		{"ab--" + strings.Repeat("c", 59) + ".example", true},
		{"ab--" + strings.Repeat("c", 60) + ".example", false},
		{"-example.com", false},
		{"example-.com", false},
		{"-.example", false},
		{"foo_bar.example", false},
		{"foo/bar.example", false},
		{"ab--cd.-example", false},
		{"ab--cd.example-", false},
		{"ab--cd..example", false},
		{"ab--cd。-example", false},
		{"ab--cd。example。", false},
		{"ab--cd.xn--", false},
		{"ab--cd.xn--a", false},
		{"XN--a.ab--cd", false},
		{"-é.example", false},
		{"é-.example", false},
		{"ab--é.example", false},
		{"ab--cd.-é", false},
		{"ab--cd.é-", false},
		{"ab--cd.ab--é", false},
		{"xn----bga.example", false},    // Punycode for -é.
		{"xn----9fa.example", false},    // Punycode for é-.
		{"xn--ab---epa.example", false}, // Punycode for ab--é.
		{"ab--cd.xn----bga", false},
		{"ab--cd.xn----9fa", false},
		{"ab--cd.xn--ab---epa", false},
		{"ab--cd.\u0301example", false},
		{"ab--cd.אa", false},
		{"ab--cd.a\u200db", false},
		{"123.עברית", false},
	} {
		t.Run(tc.domain, func(t *testing.T) {
			for _, local := range []string{"person", `"first last"`} {
				if got := ValidEmail(local + "@" + tc.domain); got != tc.valid {
					t.Errorf("ValidEmail(%q) = %v, want %v", local+"@"+tc.domain, got, tc.valid)
				}
			}
		})
	}
}

func TestValidEmailEncodedDomainLengths(t *testing.T) {
	// These equivalent labels encode to 63 bytes. Keep the expected ASCII values
	// independent of the IDNA implementation used by ValidEmail.
	unicodeLabel := strings.Repeat("界", 57)
	asciiLabel := "xn--koy" + strings.Repeat("a", 56)
	for _, tc := range []struct {
		name, unicodeDomain, asciiDomain string
		valid                            bool
	}{
		{"UTF-8 exceeds label limit", strings.Repeat("界", 22) + ".com", "xn--koy" + strings.Repeat("a", 21) + ".com", true},
		{"label boundary", unicodeLabel + ".com", asciiLabel + ".com", true},
		{"label overflow", unicodeLabel + "界.com", asciiLabel + "a.com", false},
		{"domain boundary", strings.Repeat(unicodeLabel+".", 3) + strings.Repeat("界", 55), strings.Repeat(asciiLabel+".", 3) + "xn--koy" + strings.Repeat("a", 54), true},
		{"domain overflow", strings.Repeat(unicodeLabel+".", 3) + strings.Repeat("界", 56), strings.Repeat(asciiLabel+".", 3) + "xn--koy" + strings.Repeat("a", 55), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, domain := range []string{tc.unicodeDomain, tc.asciiDomain} {
				if got := ValidEmail("person@" + domain); got != tc.valid {
					t.Errorf("ValidEmail with domain %q = %v, want %v", domain, got, tc.valid)
				}
			}
		})
	}
}

type testForm struct {
	Form
	value   string
	bindErr error
	checks  int
}

func (f *testForm) Bind(r *http.Request) error {
	f.value = r.FormValue("value")
	return f.bindErr
}

func (f *testForm) Validate() FieldErrors {
	f.checks++
	if f.value == "" {
		return FieldErrors{"Value": {RequiredMessage}}
	}
	return nil
}

func TestValidateReplacesFieldErrors(t *testing.T) {
	f := &testForm{}
	f.AddErrorMessage("Unable to save")
	for range 2 {
		if err := Validate(f); err != nil {
			t.Fatal(err)
		}
		if !f.IsValidated() || !reflect.DeepEqual(f.GetFieldErrors("Value"), []string{RequiredMessage}) {
			t.Fatal("validation did not record one field error")
		}
	}

	f.value = "corrected"
	if err := Validate(f); err != nil {
		t.Fatal(err)
	}
	if f.HasFieldErrors("Value") || len(f.GetAllFieldErrors()) != 0 {
		t.Fatal("corrected form retained old field errors")
	}
	if !f.HasErrorMessages() || !f.HasErrors() {
		t.Fatal("field validation erased the form-level error")
	}
}

func TestValidateInvalidForm(t *testing.T) {
	var nilForm *testForm
	for _, value := range []any{nil, nilForm, testForm{}, 42, new(int), &struct{}{}} {
		if err := Validate(value); err == nil {
			t.Errorf("Validate(%T) accepted an invalid form", value)
		}
	}
}

func TestParseAndValidate(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		bindErr    error
		wantError  bool
		wantField  bool
	}{
		{name: "valid", body: "value=present"},
		{name: "required", body: "value=", wantField: true},
		{name: "malformed", body: "value=%ZZ", wantError: true},
		{name: "binding failure", body: "value=present", bindErr: errors.New("bind failed"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &testForm{bindErr: tc.bindErr}
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			message, err := ParseAndValidate(r, f)
			if (err != nil) != tc.wantError || (message != "") != tc.wantError {
				t.Fatalf("ParseAndValidate() = %q, %v", message, err)
			}
			if f.IsValidated() == tc.wantError || f.HasErrors() != tc.wantField {
				t.Fatalf("validated = %v, errors = %v", f.IsValidated(), f.GetAllFieldErrors())
			}
			if tc.wantError && f.checks != 0 {
				t.Fatal("validation ran after parsing or binding failed")
			}
		})
	}
}

func TestSetFieldErrors(t *testing.T) {
	var f Form
	want := FieldErrors{"Email": {"First", "Second"}, "Name": {"Third"}}
	f.SetFieldErrors(want)
	if !reflect.DeepEqual(f.GetFieldErrors("Email"), want["Email"]) || len(f.GetAllFieldErrors()) != 3 {
		t.Fatal("field messages were not preserved")
	}
	f.SetFieldErrors(nil)
	if f.HasErrors() {
		t.Fatal("nil field errors did not clear previous errors")
	}
}
