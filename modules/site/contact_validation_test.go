package module_site

import (
	"reflect"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/form"
)

func TestContactFormValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		form ContactForm
		want form.FieldErrors
	}{
		{"valid", ContactForm{Name: "Person", Email: "person@example.com", Message: "Hello"}, form.FieldErrors{}},
		{"required", ContactForm{}, form.FieldErrors{"Name": {form.RequiredMessage}, "Email": {form.RequiredMessage}, "Message": {form.RequiredMessage}}},
		{"invalid email", ContactForm{Name: "Person", Email: "bad", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"single-label domain", ContactForm{Name: "Person", Email: "test@a", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"localhost", ContactForm{Name: "Person", Email: "person@localhost", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"dot in local part", ContactForm{Name: "Person", Email: "first.last@a", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"quoted local part without dotted domain", ContactForm{Name: "Person", Email: `"first.last@internal"@a`, Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"quoted local part with dotted domain", ContactForm{Name: "Person", Email: `"first.last@internal"@example.com`, Message: "Hello"}, form.FieldErrors{}},
		{"IPv4 literal", ContactForm{Name: "Person", Email: "person@[127.0.0.1]", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"IPv6 literal", ContactForm{Name: "Person", Email: "person@[IPv6:::1]", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"trailing dot", ContactForm{Name: "Person", Email: "person@example.", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"empty domain label", ContactForm{Name: "Person", Email: "person@example..com", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"international domain", ContactForm{Name: "Person", Email: "person@例え.テスト", Message: "Hello"}, form.FieldErrors{}},
		{"ideographic separator", ContactForm{Name: "Person", Email: "person@例え。テスト", Message: "Hello"}, form.FieldErrors{}},
		{"fullwidth separator", ContactForm{Name: "Person", Email: "person@example\uff0ecom", Message: "Hello"}, form.FieldErrors{}},
		{"halfwidth separator", ContactForm{Name: "Person", Email: "person@example\uff61com", Message: "Hello"}, form.FieldErrors{}},
		{"internal hyphens", ContactForm{Name: "Person", Email: "person@ab--cd.example", Message: "Hello"}, form.FieldErrors{}},
		{"mixed domain", ContactForm{Name: "Person", Email: "person@ab--cd.例え", Message: "Hello"}, form.FieldErrors{}},
		{"leading hyphen", ContactForm{Name: "Person", Email: "person@-example.com", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"invalid Punycode", ContactForm{Name: "Person", Email: "person@ab--cd.xn--a", Message: "Hello"}, form.FieldErrors{"Email": {form.EmailMessage}}},
		{"whitespace is not trimmed", ContactForm{Name: " ", Email: "person@example.com", Message: " "}, form.FieldErrors{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.form.Validate(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Validate() = %v, want %v", got, tc.want)
			}
		})
	}
}
