// message.go defines a ContentMessage and related methods
package messages

import (
	"net/http"
)

type (
	ContentMessage struct {
		SessionMessage
	}
)

const (
	MessageTypeDefault MessageType = "default"
	MessageTypeSuccess MessageType = "success"
	MessageTypeError   MessageType = "error"

	categoryContentMessage string = "hs-category-content-message"
)

func (m MessageType) String() string {
	return string(m)
}

// IsDefault returns true if the specified ContentMessage is a default message
func (c *ContentMessage) IsDefault() bool {
	return c.Type == MessageTypeDefault.String()
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *ContentMessage) IsSuccess() bool {
	return c.Type == MessageTypeSuccess.String()
}

// IsError returns true if the specified ContentMessage is an error message
func (c *ContentMessage) IsError() bool {
	return c.Type == MessageTypeError.String()
}

func NewContentMessage(message string, messageType MessageType) ContentMessage {
	return ContentMessage{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}

// AddMessage adds a default ContentMessage to a session
func AddMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addContentMessage(w, r, NewContentMessage(message, MessageTypeDefault))
}

// AddErrorMessage adds an error ContentMessage to a session
func AddErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addContentMessage(w, r, NewContentMessage(message, MessageTypeError))
}

// AddSuccessMessage adds a success ContentMessage to a session
func AddSuccessMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addContentMessage(w, r, NewContentMessage(message, MessageTypeSuccess))
}

// GetContentMessages returns the slice of ContentMessage instances that have been saved to
// a session. The messages (and the session they are contained in) are then deleted,
// meaning that this function is not idempotent.
func GetContentMessages(w http.ResponseWriter, r *http.Request) ([]ContentMessage, error) {
	msgs, err := getMessages(w, r, categoryContentMessage)
	if err != nil {
		return nil, err
	}

	var contentMessages []ContentMessage
	for _, msg := range msgs {
		contentMessage := NewContentMessage(msg.Message, MessageType(msg.Type))
		contentMessages = append(contentMessages, contentMessage)
	}

	return contentMessages, nil
}

// addContentMessage serializes the specified message to a session used explicitly for storing
// content messages that can be accessed by handlers
func addContentMessage(w http.ResponseWriter, r *http.Request, c ContentMessage) error {
	return addMessage(w, r, c.Message, c.Type, categoryContentMessage)
}
