package module_site

import (
	"errors"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
)

// ContactForm binds the development site's contact submission fields.
type (
	ContactForm struct {
		Name    string
		Email   string
		Message string

		form.Form
	}
)

const (
	contactPageTemplate = "templates/html/pages/contact.html"
	contactFormTemplate = "templates/html/partials/contact-form.html"
)

// Validate checks the required contact fields and email address.
func (cf *ContactForm) Validate() form.FieldErrors {
	errors := make(form.FieldErrors)
	if cf.Name == "" {
		errors["Name"] = []string{form.RequiredMessage}
	}
	if cf.Email == "" {
		errors["Email"] = []string{form.RequiredMessage}
	} else if !validContactEmail(cf.Email) {
		errors["Email"] = []string{form.EmailMessage}
	}
	if cf.Message == "" {
		errors["Message"] = []string{form.RequiredMessage}
	}
	return errors
}

func validContactEmail(value string) bool {
	if !form.ValidEmail(value) {
		return false
	}

	// Public contact addresses must have a dotted domain name, not an IP literal.
	// Include the Unicode separators recognized by IDNA domain validation.
	domain := value[strings.LastIndexByte(value, '@')+1:]
	return !strings.HasPrefix(domain, "[") && strings.ContainsAny(domain, ".\u3002\uff0e\uff61")
}

// Bind populates the contact fields from the request.
func (cf *ContactForm) Bind(r *http.Request) error {
	cf.Name = r.FormValue("name")
	cf.Email = r.FormValue("email")
	cf.Message = r.FormValue("message")
	return nil
}

func (m *SiteModule) contactContent(r *http.Request, data *ContactForm) *content.Content {
	request := htmx.GetRequest(r)
	if request.Enabled && !request.HistoryRestore {
		return m.views[contactFormPartialName].Content(r, data)
	}
	page := m.views[contactPagePartialName].Content(r, data)
	page.Title = "Contact Us"
	return page
}

// GetContact retrieves an empty contact form
func (m *SiteModule) GetContact(w http.ResponseWriter, r *http.Request) {
	contact := m.contactContent(r, &ContactForm{})

	err := contact.Render(w, r)
	if err != nil {
		contact.HandleRenderError(w, r, "could not render the contact form", err)
	}
}

// Contact handles a contact submission
func (m *SiteModule) Contact(w http.ResponseWriter, r *http.Request) {
	contactForm := &ContactForm{}
	validationErr := form.ParseAndValidate(r, contactForm)
	var parseErr *form.ParseError
	if errors.As(validationErr, &parseErr) {
		form.HandleFormError(w, r, nil, contactForm, "", validationErr)
		return
	}

	contact := m.contactContent(r, contactForm)

	if validationErr != nil {
		form.HandleFormError(
			w,
			r,
			contact,
			contactForm,
			"The contact form could not be validated",
			validationErr)
		return
	}

	// add the contact submission to the database
	cs := models.ContactSubmission{
		Name:    contactForm.Name,
		Email:   contactForm.Email,
		Message: contactForm.Message,
	}

	err := m.contacts.Create(r.Context(), &cs)
	if err != nil {
		form.HandleFormError(w, r, contact, contactForm, "Unable to save your message", err)
		return
	}

	contactForm.Name = ""
	contactForm.Email = ""
	contactForm.Message = ""
	contactForm.AddSuccessMessage("Your message has been submitted.")

	// render the success response
	err = contact.Render(w, r)
	if err != nil {
		contact.HandleRenderError(w, r, "could not render the contact form after successfully adding a contact", err)
	}
}
