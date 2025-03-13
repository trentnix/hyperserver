// message.go defines a ContentMessage and related methods
package content

import (
	"encoding/json"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	ContentMessage struct {
		Message     string `json:"message"`
		MessageType string `json:"messageType"`
	}

	sessionKey  string
	MessageType string
)

const (
	messageSessionKey sessionKey = "hs-message-session"

	MessageTypeDefault MessageType = "default"
	MessageTypeSuccess MessageType = "success"
	MessageTypeError   MessageType = "error"
)

// IsDefault returns true if the specified ContentMessage is a default message
func (c *ContentMessage) IsDefault() bool {
	return c.MessageType == string(MessageTypeDefault)
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *ContentMessage) IsSuccess() bool {
	return c.MessageType == string(MessageTypeSuccess)
}

// IsError returns true if the specified ContentMessage is an error message
func (c *ContentMessage) IsError() bool {
	return c.MessageType == string(MessageTypeError)
}

func NewContentMessage(message string, messageType MessageType) ContentMessage {
	return ContentMessage{
		Message:     message,
		MessageType: string(messageType),
	}
}

// AddUserMessage adds an error ContentMessage to the request context
func AddMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeDefault))
}

// AddUserErrorMessage adds an error ContentMessage to the request context
func AddErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeError))
}

// AddUserSuccessMessage adds an success ContentMessage to the request context
func AddSuccessMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeSuccess))
}

// addMessage serializes the specified message to a session used explicitly for storing
// content messages that can be accessed by handlers
func addMessage(w http.ResponseWriter, r *http.Request, c ContentMessage) error {
	sessionManager := session.GetSessionManager()
	s, err := sessionManager.Get(r, string(messageSessionKey))
	if err != nil {
		return NewErrRetrievingContentMessages(err)
	}

	sContentMessages := s.Data[string(messageSessionKey)]

	var convertErr error
	contentMessages, convertErr := ConvertContentMessagesFromJSON(sContentMessages)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}

	contentMessages = append(contentMessages, c)

	sContentMessages, convertErr = ConvertContentMessagesToJSON(contentMessages)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}

	s.Data[string(messageSessionKey)] = sContentMessages
	err = s.Save(r, w)
	if err != nil {
		return NewErrSavingContentMessages(err)
	}

	return nil
}

// RetrieveMessages returns the slice of ContentMessage instances that may
// have been saved to a session and then deletes the associated messages from
// the session. Once this is called, the session is no longer valid.
func RetrieveMessages(r *http.Request, w http.ResponseWriter) ([]ContentMessage, error) {
	sessionManager := session.GetSessionManager()
	s, err := sessionManager.Get(r, string(messageSessionKey))
	if err != nil {
		return nil, NewErrRetrievingContentMessages(err)
	}

	sContentMessages := s.Data[string(messageSessionKey)]

	var convertErr error
	contentMessages, convertErr := ConvertContentMessagesFromJSON(sContentMessages)
	if convertErr != nil {
		return nil, NewErrConvertingContentMessagesData(convertErr)
	}

	deleteMessagesErr := s.End(r, w)

	return contentMessages, deleteMessagesErr
}

// ConvertContentMessagesToJSON takes a slice of ContentMessage and returns a JSON string.
func ConvertContentMessagesToJSON(messages []ContentMessage) (string, error) {
	data, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ConvertContentMessagesFromJSON takes a JSON string and returns a slice of ContentMessage.
func ConvertContentMessagesFromJSON(jsonString string) ([]ContentMessage, error) {
	var messages []ContentMessage
	if jsonString == "" {
		return messages, nil
	}

	err := json.Unmarshal([]byte(jsonString), &messages)
	if err != nil {
		return nil, err
	}
	return messages, nil
}
