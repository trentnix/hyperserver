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
	contact := content.NewManagedContent(r, m.contentManager)

	if !contact.IsHtmx() {
		// we need to load the form in a page - load the page that will host the form
		contact.AddLayoutTemplate(content.Template(contactPageTemplate))
	}

	contact.AddContentTemplate(content.Template(contactFormTemplate))
	contact.Data = &ContactForm{}

	err := contact.Render(w)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}

// Contact handles a contact submission
func (m *SiteModule) Contact(w http.ResponseWriter, r *http.Request) {
	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("There was an error parsing the contact form: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	contact := content.NewManagedContent(r, m.contentManager)

	if !contact.IsHtmx() {
		// we need to load the form in a page - load the page that will host the form
		contact.AddLayoutTemplate(content.Template(contactPageTemplate))
	}

	contact.AddContentTemplate(content.Template(contactFormTemplate))

	// process a contact submission
	contactForm := &ContactForm{}
	contactForm.Name = r.FormValue("name")
	contactForm.Email = r.FormValue("email")
	contactForm.Message = r.FormValue("message")

	// validate the resetPasswordRequest form
	err := form.ValidateForm(contactForm)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error validating the contact form: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	if contactForm.HasErrors() {
		contact.Data = contactForm
		err = contact.Render(w)
		if err != nil {
			http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
		}
	}

	// TO DO: save the form details to the database

	// if save successful

	contactForm.Name = r.FormValue("")
	contactForm.Email = r.FormValue("")
	contactForm.Message = r.FormValue("")
	contactForm.SetFormMessage("<need a success message>")

	contact.Data = contactForm
	err = contact.Render(w)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}
