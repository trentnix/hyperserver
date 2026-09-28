package module_site

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/content"
)

func TestContactFormMessages(t *testing.T) {
	tmpl, err := template.ParseFiles(
		"templates/html/partials/contact-form.html",
		"templates/html/pages/contact.html",
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{contactFormPartialName, contactPagePartialName} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				success string
				errMsg  string
				want    string
			}{
				{name: "initial form"},
				{name: "saved submission", success: "Your message has been submitted.", want: "Your message has been submitted."},
				{name: "escaped success", success: "<script>alert(1)</script>", want: "&lt;script&gt;alert(1)&lt;/script&gt;"},
				{name: "failed submission", errMsg: "Unable to save your message", want: "Unable to save your message"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					form := &ContactForm{}
					if tc.success != "" {
						form.AddSuccessMessage(tc.success)
					}
					if tc.errMsg != "" {
						form.AddErrorMessage(tc.errMsg)
					}
					var output bytes.Buffer
					if err := tmpl.ExecuteTemplate(&output, name, &content.Content{Data: form}); err != nil {
						t.Fatal(err)
					}
					html := output.String()
					if tc.want != "" && !strings.Contains(html, tc.want) {
						t.Errorf("rendered form is missing %q", tc.want)
					}
					if got, want := strings.Contains(html, `role="status"`), tc.success != ""; got != want {
						t.Errorf("success status present = %v, want %v", got, want)
					}
					if strings.Contains(html, "<script>") {
						t.Error("success message contains unescaped HTML")
					}
				})
			}
		})
	}
}
