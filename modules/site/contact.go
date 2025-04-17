// contact.go handles interactions with the contact form on the site
package module_site

import (
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

// Bind populates the RegisterForm fields from the request.
func (cf *ContactForm) Bind(r *http.Request) error {
	cf.Name = r.FormValue("name")
	cf.Email = r.FormValue("email")
	cf.Message = r.FormValue("message")
	return nil
}

// GetContact retrieves an empty contact form
func (m *SiteModule) GetContact(w http.ResponseWriter, r *http.Request) {
	contact := content.NewManagedContent(r)

	if !contact.IsHtmx() {
		// we need to load the form in a page - load the page that will host the form
		contact.AddLayout(contactPageTemplate)
		contact.Title = "Contact Us"
	}

	contact.AddContent(contactFormTemplate)
	contact.Data = &ContactForm{}

	err := contact.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r, "could not render the contact form", err, http.StatusInternalServerError)
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
		contact.AddLayout(contactPageTemplate)
		contact.Title = "Contact Us"
	}

	contact.AddContent(contactFormTemplate)

	// process a contact submission
	contactForm := &ContactForm{}
	contactForm.Name = r.FormValue("name")
	contactForm.Email = r.FormValue("email")
	contactForm.Message = r.FormValue("message")

	// validate the ContactForm form
	err := form.Validate(contactForm)
	if err != nil {
		form.HandleFormError(
			w,
			r,
			contact,
			contactForm,
			"The login form could not be validated",
			err)
		return
	}

	if contactForm.HasErrors() {
		// there are validation errors - render the form errors
		form.HandleFormError(w, r, contact, contactForm, "", nil)
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
		form.HandleFormError(w, r, contact, contactForm, "Unable to save your message", err)
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
		m.contentManager.HandleError(w, r, "could not render the contact form after successfully adding a contact", err, http.StatusInternalServerError)
	}
}
