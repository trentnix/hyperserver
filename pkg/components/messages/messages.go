// messages.go defines the interaction session management and flash messages. A slice of Message instances
// is stored in a session explicitly for flash messages. Each message has a message value and a message type.
// Messages are stored in categories, allowing different message categories to be managed and retrieved
// separately.
package messages

import (
	"encoding/json"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	SessionMessage struct {
		Message string `json:"message"`
		Type    string `json:"messageType"`
	}

	SessionKey  string
	MessageType string
)

const (
	messagesSession SessionKey = "hs-message-session"
)

func (s SessionKey) String() string {
	return string(s)
}

// newMessage creates a new instance of a Message
func newMessage(message string, messageType string) SessionMessage {
	return SessionMessage{
		Message: message,
		Type:    messageType,
	}
}

// addMessage creates a Message (using the message and messageType values) and adds it to the
// specified category in the messages session
func addMessage(w http.ResponseWriter, r *http.Request, message string, messageType string, category string) error {
	s, err := session.Get(r, messagesSession.String())
	if err != nil {
		return NewErrRetrievingContentMessages(err)
	}

	msgData := s.Data[category]
	msgs, convertErr := messagesFromJSON(msgData)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}

	m := newMessage(message, messageType)
	msgs = append(msgs, m)

	msgData, convertErr = messagesToJSON(msgs)
	if convertErr != nil {
		return NewErrConvertingContentMessagesData(convertErr)
	}
	s.Data[category] = msgData

	err = s.Save(w, r)
	if err != nil {
		return NewErrSavingContentMessages(err)
	}

	return nil
}

// getMessages retrieves (and removes) the Messages from the specified category from the
// Messages session.
func getMessages(w http.ResponseWriter, r *http.Request, category string) ([]SessionMessage, error) {
	s, err := session.Get(r, messagesSession.String())
	if err != nil {
		return nil, NewErrRetrievingContentMessages(err)
	}

	sContentMessages := s.Data[category]

	var convertErr error
	contentMessages, convertErr := messagesFromJSON(sContentMessages)
	if convertErr != nil {
		return nil, NewErrConvertingContentMessagesData(convertErr)
	}

	delete(s.Data, category)

	if len(s.Data) == 0 {
		deleteMessagesErr := s.End(w, r)
		if deleteMessagesErr != nil {
			return contentMessages, deleteMessagesErr
		}
	}

	return contentMessages, nil
}

// messagesToJSON takes a slice of ContentMessage and returns a JSON string.
func messagesToJSON(messages []SessionMessage) (string, error) {
	if messages == nil {
		return "", nil
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// messagesFromJSON takes a JSON string and returns a slice of ContentMessage.
func messagesFromJSON(jsonString string) ([]SessionMessage, error) {
	var messages []SessionMessage
	if jsonString == "" {
		return messages, nil
	}

	err := json.Unmarshal([]byte(jsonString), &messages)
	if err != nil {
		return nil, err
	}
	return messages, nil
}
