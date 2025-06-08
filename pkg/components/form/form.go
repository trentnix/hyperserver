// form.go defines the FormComponent interface and provides an implementation of the interface that
// can be used by structures that represent form data in the application.
package form

import (
	"fmt"
	"html/template"

	"github.com/go-playground/validator/v10"
	"github.com/trentnix/hyperserver/pkg/components/messages"
)

// Form is an implementation of the FormComponent interface
type Form struct {
	isValidated bool
	messages    []messages.Message
	fieldErrors map[string][]string

	ActionUrl string
}

// IsValidated specifies whether the form has been validated (but not whether any errors were found)
func (f *Form) IsValidated() bool {
	return f.isValidated
}

// SetValidated sets the form status to having been validated or not
func (f *Form) SetValidated(v bool) {
	f.isValidated = v
}

// GetFieldErrors gets the errors messages for the specified field
func (f *Form) GetFieldErrors(field string) []string {
	if f.fieldErrors == nil {
		return []string{}
	}
	return f.fieldErrors[field]
}

// GetAllFieldErrors gets all the field error messages in a single slice
func (f *Form) GetAllFieldErrors() []string {
	var allErrors []string
	if f.fieldErrors == nil {
		return allErrors
	}

	for _, errors := range f.fieldErrors {
		allErrors = append(allErrors, errors...)
	}

	return allErrors
}

// SetFieldErrors parses the specified field-level validation errors and adds them to the fieldErrors map
func (f *Form) SetFieldErrors(err error) {
	ves, ok := err.(validator.ValidationErrors)
	if !ok {
		return
	}

	for _, ve := range ves {
		var message string

		// Provide better error messages depending on the failed validation tag
		// This should be expanded as you use additional tags in your validation
		switch ve.Tag() {
		case "required":
			message = "This field is required."
		case "email":
			message = "Enter a valid email address."
		case "password":
			message = "Password values must contain at least 8 characters, one letter, one number, and one special character"
		case "eqfield":
			message = "Passwords do not match."
		case "nefield":
			message = "New passwords must be different from the previous password."
		case "gte":
			message = fmt.Sprintf("Must be greater than or equal to %v.", ve.Param())
		default:
			message = "Invalid value."
		}

		// Add the error
		if f.fieldErrors == nil {
			f.fieldErrors = make(map[string][]string)
		}

		f.fieldErrors[ve.Field()] = append(f.fieldErrors[ve.Field()], message)
	}
}

// HasFieldErrors returns true if the specified field has errors
func (f *Form) HasFieldErrors(field string) bool {
	return len(f.GetFieldErrors(field)) > 0
}

// HasErrorMessages returns true if the form has form-level error messages
func (f *Form) HasErrorMessages() bool {
	return len(f.GetErrorMessages()) > 0
}

// HasSuccessMessages returns true if the form has form-level success messages
func (f *Form) HasSuccessMessages() bool {
	return len(f.GetSuccessMessages()) > 0
}

// HasInfoMessages returns true if the form has form-level notifications
func (f *Form) HasInfoMessages() bool {
	return len(f.GetInfoMessages()) > 0
}

// GetFormError gets a form-level error message
func (f *Form) GetErrorMessages() []messages.Message {
	return f.getMessages(messages.MessageTypeError)
}

// GetSuccessMessage returns a form message to display
func (f *Form) GetSuccessMessages() []messages.Message {
	return f.getMessages(messages.MessageTypeSuccess)
}

// GetInfoMessages returns a form message to display
func (f *Form) GetInfoMessages() []messages.Message {
	return f.getMessages(messages.MessageTypeDefault)
}

// getMessages retrieves just the messages from Form.messages that match the
// specified type
func (f *Form) getMessages(msgType messages.MessageType) []messages.Message {
	var msgs []messages.Message
	for _, m := range f.messages {
		if m.Type == string(msgType) {
			msgs = append(msgs, m)
		}
	}

	return msgs
}

// AddErrorMessage adds messages.Message instance with an error type to the Form
func (f *Form) AddErrorMessage(msg string) {
	f.addMessage(msg, messages.MessageTypeError)
}

// AddSuccessMessage adds messages.Message instance with a success type to the Form
func (f *Form) AddSuccessMessage(msg string) {
	f.addMessage(msg, messages.MessageTypeSuccess)
}

// AddMessage adds messages.Message instance with a default (non-error, non-success)
// type to the Form
func (f *Form) AddMessage(msg string) {
	f.addMessage(msg, messages.MessageTypeDefault)
}

// addMessage handles appending a message to the Form.messages slice
func (f *Form) addMessage(msg string, msgType messages.MessageType) {
	f.messages = append(f.messages, messages.NewMessage(msg, msgType))
}

// SetMessages takes a slice of messages.Message values and splits them into
// Form.Errors and Form.Messages
func (f *Form) SetMessages(formMessages []messages.Message) {
	f.messages = formMessages
}

// GetErrorMessagesHTML returns an HTML result for just error messages
func (f *Form) GetErrorMessagesHTML() template.HTML {
	return f.getMessagesHTML(messages.MessageTypeError)
}

// GetSuccessMessagesHTML returns an HTML result for just success messages
func (f *Form) GetSuccessMessagesHTML() template.HTML {
	return f.getMessagesHTML(messages.MessageTypeSuccess)
}

// GetInfoMessagesHTML returns an HTML result for just informational messages
func (f *Form) GetInfoMessagesHTML() template.HTML {
	return f.getMessagesHTML(messages.MessageTypeDefault)
}

// getMessagesHTML retrieves a slice of messages from Form.messages that
// match the specified type
func (f *Form) getMessagesHTML(msgType messages.MessageType) template.HTML {
	var msgs []messages.Message
	switch msgType {
	case messages.MessageTypeError:
		msgs = f.GetErrorMessages()
	case messages.MessageTypeSuccess:
		msgs = f.GetSuccessMessages()
	default:
		msgs = f.GetInfoMessages()
	}

	var htmlMessages string
	htmlMessages = "<ul>"
	for _, m := range msgs {
		htmlMessages += fmt.Sprintf("<li>%s</li>", m.Message)
	}
	htmlMessages += "</ul>"

	return template.HTML(htmlMessages)
}
