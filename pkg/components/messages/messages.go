// messages.go defines the interaction session management and flash messages. A slice of Message instances
// is stored in a session explicitly for flash messages. Each message has a message value and a message type.
// Messages are stored in categories, allowing different message categories to be managed and retrieved
// separately.
package messages

import (
	"encoding/gob"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	SessionMessage struct {
		Message string `json:"message"`
		Type    string `json:"messageType"`
	}

	SessionKey string
)

const (
	messagesSession SessionKey = "hs-message-session"
)

func init() {
	gob.Register([]SessionMessage{}) // needed for securecookie/gob encoding
}

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

	m := newMessage(message, messageType)

	msgs, _ := s.Data[category].([]SessionMessage)
	msgs = append(msgs, m)
	s.Data[category] = msgs

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

	contentMessages, _ := s.Data[category].([]SessionMessage)
	delete(s.Data, category)

	var deleteMessagesErr error
	if len(s.Data) == 0 {
		deleteMessagesErr = s.End(w, r)
	}

	return contentMessages, deleteMessagesErr
}
