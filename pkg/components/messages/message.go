// message.go defines a ContentMessage and related methods
package messages

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

	SessionKey  string
	MessageType string
)

const (
	messageSessionKey SessionKey = "hs-message-session"

	MessageTypeDefault MessageType = "default"
	MessageTypeSuccess MessageType = "success"
	MessageTypeError   MessageType = "error"
)

func (m MessageType) String() string {
	return string(m)
}

func (s SessionKey) String() string {
	return string(s)
}

// IsDefault returns true if the specified ContentMessage is a default message
func (c *ContentMessage) IsDefault() bool {
	return c.MessageType == MessageTypeDefault.String()
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *ContentMessage) IsSuccess() bool {
	return c.MessageType == MessageTypeSuccess.String()
}

// IsError returns true if the specified ContentMessage is an error message
func (c *ContentMessage) IsError() bool {
	return c.MessageType == MessageTypeError.String()
}

func NewContentMessage(message string, messageType MessageType) ContentMessage {
	return ContentMessage{
		Message:     message,
		MessageType: messageType.String(),
	}
}

// AddMessage adds a default ContentMessage to a session
func AddMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeDefault))
}

// AddErrorMessage adds an error ContentMessage to a session
func AddErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeError))
}

// AddSuccessMessage adds a success ContentMessage to a session
func AddSuccessMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addMessage(w, r, NewContentMessage(message, MessageTypeSuccess))
}

// GetMessages returns the slice of ContentMessage instances that have been saved to
// a session. The messages (and the session they are contained in) are then deleted,
// meaning that this function is not idempotent.
func GetMessages(w http.ResponseWriter, r *http.Request) ([]ContentMessage, error) {
	s, err := session.Get(r, messageSessionKey.String())
	if err != nil {
		return nil, NewErrRetrievingContentMessages(err)
	}

	sContentMessages := s.Data[string(messageSessionKey)]

	var convertErr error
	contentMessages, convertErr := contentMessagesFromJSON(sContentMessages)
	if convertErr != nil {
		return nil, NewErrConvertingContentMessagesData(convertErr)
	}

	deleteMessagesErr := s.End(w, r)

	return contentMessages, deleteMessagesErr
}

// addMessage serializes the specified message to a session used explicitly for storing
// content messages that can be accessed by handlers
func addMessage(w http.ResponseWriter, r *http.Request, c ContentMessage) error {
	s, err := session.Get(r, messageSessionKey.String())
	if err != nil {
		return NewErrRetrievingContentMessages(err)
	}

	msgData := s.Data[messageSessionKey.String()]

	msgs, convertErr := contentMessagesFromJSON(msgData)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}

	msgs = append(msgs, c)

	msgData, convertErr = contentMessagesToJSON(msgs)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}

	s.Data[string(messageSessionKey)] = msgData
	err = s.Save(w, r)
	if err != nil {
		return NewErrSavingContentMessages(err)
	}

	return nil
}

// contentMessagesToJSON takes a slice of ContentMessage and returns a JSON string.
func contentMessagesToJSON(messages []ContentMessage) (string, error) {
	if messages == nil {
		return "", nil
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// contentMessagesFromJSON takes a JSON string and returns a slice of ContentMessage.
func contentMessagesFromJSON(jsonString string) ([]ContentMessage, error) {
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
