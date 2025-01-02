// contact.go handles interactions with the contact form on the site
package module_site

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/components/content"
	"github.com/trentnix/hyperserver/components/form"
)

// contactForm defines the fields used when logging in via email/password
type (
	ContactForm struct {
		Name    string `validate:"required"`
		Email   string `validate:"required,email"`
		Message string `validate:"required"`

		form.Form
	}
)

const (
	contactPageTemplate = "modules/site/templates/html/contact.html"
	contactFormTemplate = "modules/site/templates/html/contact-form.html"
)

// GetContact retrieves an empty contact form
func (m *SiteModule) GetContact(w http.ResponseWriter, r *http.Request) {
	contactForm := ContactForm{}

	contact := content.NewManagedContent(r, m.contentManager)
	contact.AddTemplate(content.Template(contactPageTemplate))
	contact.AddTemplate(content.Template(contactFormTemplate))
	contact.Data = &contactForm

	err := contact.Render(w)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}

// Contact handles a contact submission
func (m *SiteModule) Contact(w http.ResponseWriter, r *http.Request) {
	// process a contact submission
}
