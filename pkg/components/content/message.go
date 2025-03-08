// message.go defines a ContentMessage and related methods
package content

import (
	"context"
	"net/http"
)

type (
	ContentMessage struct {
		Message     string
		MessageType string
	}

	contextKey string
)

const (
	messagesKey contextKey = "contentMessages"

	messageTypeDefault = "default"
	messageTypeSuccess = "success"
	messageTypeError   = "error"
)

// IsDefault returns true if the specified ContentMessage is a default message
func (c *ContentMessage) IsDefault() bool {
	return c.MessageType == messageTypeDefault
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *ContentMessage) IsSuccess() bool {
	return c.MessageType == messageTypeSuccess
}

// IsError returns true if the specified ContentMessage is an error message
func (c *ContentMessage) IsError() bool {
	return c.MessageType == messageTypeError
}

// AddUserMessage adds an error ContentMessage to the request context
func AddUserMessage(r *http.Request, message string) *http.Request {
	c := ContentMessage{
		Message:     message,
		MessageType: messageTypeDefault,
	}

	return addContentMessageToRequestContext(r, c)
}

// AddUserErrorMessage adds an error ContentMessage to the request context
func AddUserErrorMessage(r *http.Request, message string) *http.Request {
	c := ContentMessage{
		Message:     message,
		MessageType: messageTypeError,
	}

	return addContentMessageToRequestContext(r, c)
}

// AddUserSuccessMessage adds an success ContentMessage to the request context
func AddUserSuccessMessage(r *http.Request, message string) *http.Request {
	c := ContentMessage{
		Message:     message,
		MessageType: messageTypeSuccess,
	}

	return addContentMessageToRequestContext(r, c)
}

// GetContentMessages returns the slice of ContentMessage instances that may
// have been saved to the request context of the specified request
func GetContentMessages(r *http.Request) []ContentMessage {
	val := r.Context().Value(messagesKey)
	msgs, ok := val.([]ContentMessage)
	if !ok {
		return nil
	}

	return msgs
}

// addContentMessageToRequestContext adds the specified ContentMessage to the request
// context
func addContentMessageToRequestContext(r *http.Request, contentMessage ContentMessage) *http.Request {
	// get any existing slice from context
	val := r.Context().Value(messagesKey)
	messages, ok := val.([]ContentMessage)
	if !ok {
		messages = []ContentMessage{}
	}

	messages = append(messages, contentMessage)

	ctx := context.WithValue(r.Context(), messagesKey, messages)

	return r.WithContext(ctx)
}
