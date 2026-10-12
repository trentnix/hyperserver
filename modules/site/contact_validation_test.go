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
