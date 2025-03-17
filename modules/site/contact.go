// contact.go handles interactions with the contact form on the site
package module_site

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
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
	contact := content.NewManagedContent(r)

	if !contact.IsHtmx() {
		// we need to load the form in a page - load the page that will host the form
		contact.AddLayout(content.TemplatePath(contactPageTemplate))
		contact.Title = "Contact Us"
	}

	contact.AddContent(content.TemplatePath(contactFormTemplate))
	contact.Data = &ContactForm{}

	err := contact.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the contact form", err, http.StatusInternalServerError)
	}
}

// Contact handles a contact submission
func (m *SiteModule) Contact(w http.ResponseWriter, r *http.Request) {
	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		return
	}

	contact := content.NewManagedContent(r)
	if !contact.IsHtmx() {
		// we need to load the form in a page - load the page that will host the form
		contact.AddLayout(content.TemplatePath(contactPageTemplate))
		contact.Title = "Contact Us"
	}

	contact.AddContent(content.TemplatePath(contactFormTemplate))

	// process a contact submission
	contactForm := &ContactForm{}
	contactForm.Name = r.FormValue("name")
	contactForm.Email = r.FormValue("email")
	contactForm.Message = r.FormValue("message")

	// validate the ContactForm form
	err := form.Validate(contactForm)
	if err != nil {
		content.HandleFormError(w, r, contact, contactForm, "The login form could not be validated")
		return
	}

	if contactForm.HasErrors() {
		// there are validation errors - render the form errors
		content.HandleFormError(w, r, contact, contactForm, "")
		return
	}

	// add the contact submission to the database
	cs := models.ContactSubmission{
		Name:    contactForm.Name,
		Email:   contactForm.Email,
		Message: contactForm.Message,
	}

	err = cs.Create(m.Database)
	if err != nil {
		content.HandleFormError(w, r, contact, contactForm, fmt.Sprintf("There was an error creating the specified contact message: %v", err.Error()))
		return
	}

	contactForm.Name = r.FormValue("")
	contactForm.Email = r.FormValue("")
	contactForm.Message = r.FormValue("")
	contactForm.SetFormMessage("Your message has been submitted.")

	// render the success response
	contact.Data = contactForm

	err = contact.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the contact form after successfully adding a contact", err, http.StatusInternalServerError)
	}
}
