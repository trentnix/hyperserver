// Package messages stores categorized flash messages, notifications, and browser
// console messages in sessions. Its HTTP helpers require session management in
// the request context. Successful retrieval persists category removal before
// returning messages. Failed removal leaves the request-local category intact.
package messages

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SessionMessage is the stored text and type shared by message categories.
	SessionMessage struct {
		Message string `json:"message"`
		Type    string `json:"messageType"`
	}

	// SessionKey names a session used for messages.
	SessionKey string
)

const (
	messagesSession SessionKey = "hs-message-session"
)

// String returns the underlying string value.
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

	var msgs []SessionMessage
	if err := s.DecodeValue(category, &msgs); err != nil {
		return NewErrRetrievingContentMessages(err)
	}
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

	stored, exists := s.Data[category]
	if !exists {
		return nil, nil
	}
	var contentMessages []SessionMessage
	if err := s.DecodeValue(category, &contentMessages); err != nil {
		return nil, NewErrRetrievingContentMessages(err)
	}
	delete(s.Data, category)

	if len(s.Data) == 0 {
		err = s.End(w, r)
	} else {
		err = s.Save(w, r)
	}
	if err != nil {
		s.Data[category] = stored
		return nil, NewErrDeletingContentMessages(err)
	}

	return contentMessages, nil
}
