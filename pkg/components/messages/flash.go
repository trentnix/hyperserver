package messages

import (
	"fmt"
	"net/http"
)

type (
	// Message is a categorized flash message stored in a session.
	Message struct {
		SessionMessage
	}

	// MessageType identifies an informational, success, or error flash message.
	MessageType string
)

const (
	// MessageTypeDefault identifies an informational flash message.
	MessageTypeDefault MessageType = "default"
	// MessageTypeSuccess identifies a successful operation.
	MessageTypeSuccess MessageType = "success"
	// MessageTypeError identifies a failed operation.
	MessageTypeError MessageType = "error"

	// value appended to flash message categories to mitigate the potential over overlap with
	// session names that might be used in the code
	sessionFlashMessagePrefix string = "hs-message-category-flash-message"

	// AuthMessages is the flash-message category used by authentication forms.
	AuthMessages = "hs_auth"
)

// String returns the underlying string value.
func (m MessageType) String() string {
	return string(m)
}

// IsDefault returns true if the specified ContentMessage is a default message
func (c *Message) IsDefault() bool {
	return c.Type == MessageTypeDefault.String()
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *Message) IsSuccess() bool {
	return c.Type == MessageTypeSuccess.String()
}

// IsError returns true if the specified ContentMessage is an error message
func (c *Message) IsError() bool {
	return c.Type == MessageTypeError.String()
}

// NewMessage creates a new instance of a Message struct
func NewMessage(message string, messageType MessageType) Message {
	return Message{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}

// AddMessage adds a default ContentMessage to a session
func AddMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, NewMessage(message, MessageTypeDefault), category)
}

// AddErrorMessage adds an error ContentMessage to a session
func AddErrorMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, NewMessage(message, MessageTypeError), category)
}

// AddSuccessMessage adds a success ContentMessage to a session
func AddSuccessMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, NewMessage(message, MessageTypeSuccess), category)
}

// GetMessages retrieves a category and persists its removal, ending an empty session.
// A failed removal returns no messages and leaves the request-local category intact.
func GetMessages(w http.ResponseWriter, r *http.Request, category string) ([]Message, error) {
	if category == "" {
		return nil, NewErrMessageCategoryNotSpecified(nil)
	}

	flashMessages, err := getMessages(w, r, flashMessageCategory(category))
	if err != nil {
		return nil, err
	}

	var msgs []Message
	for _, msg := range flashMessages {
		flashMessage := NewMessage(msg.Message, MessageType(msg.Type))
		msgs = append(msgs, flashMessage)
	}

	return msgs, nil
}

// addFlashMessage serializes the specified message to a session used explicitly for storing
// content messages that can be accessed by handlers. The message is stored by category and can
// be retrieved by category
func addFlashMessage(w http.ResponseWriter, r *http.Request, m Message, c string) error {
	return addMessage(w, r, m.Message, m.Type, flashMessageCategory(c))
}

func flashMessageCategory(category string) string {
	return fmt.Sprintf("%s-%s", sessionFlashMessagePrefix, category)
}
