// Package flash stores categorized flash messages, notifications, and browser
// console messages in sessions. Its HTTP helpers require session management in
// the request context. Get helpers remove messages immediately. LoadNotifications
// defers removal until rendering succeeds. HEAD requests do not retrieve messages.
package flash

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/trentnix/hyperserver/pkg/components/messages"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
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

// ValidateSessionManager checks that flash messages have a selected store.
// Call during startup. It does not read or write session data.
func ValidateSessionManager(manager *session.SessionManager) error {
	if _, err := manager.Store(messagesSession.String()); err != nil {
		return fmt.Errorf("flash messages require session %q: %w", messagesSession, err)
	}
	return nil
}

// newMessage creates a new instance of a Message
func newMessage(message string, messageType string) messages.SessionMessage {
	return messages.SessionMessage{
		Message: message,
		Type:    messageType,
	}
}

// addMessage creates a Message (using the message and messageType values) and adds it to the
// specified category in the messages session
func addMessage(w http.ResponseWriter, r *http.Request, message string, messageType string, category string) error {
	s, err := session.Get(r, messagesSession.String())
	if err != nil {
		return hs_errors.WithCause(ErrRetrievingContentMessages, err)
	}

	m := newMessage(message, messageType)

	var msgs []messages.SessionMessage
	if err := s.DecodeValue(category, &msgs); err != nil {
		return hs_errors.WithCause(ErrRetrievingContentMessages, err)
	}
	msgs = append(msgs, m)
	s.Data[category] = msgs

	err = s.Save(w, r)
	if err != nil {
		return hs_errors.WithCause(ErrSavingContentMessages, err)
	}

	return nil
}

// getMessages retrieves (and removes) the Messages from the specified category from the
// Messages session.
func getMessages(w http.ResponseWriter, r *http.Request, category string) ([]messages.SessionMessage, error) {
	msgs, acknowledge, err := loadMessages(r, category)
	if err != nil {
		return nil, err
	}
	if acknowledge != nil {
		if err := acknowledge(w); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// loadMessages returns a snapshot and a request-local removal operation.
func loadMessages(r *http.Request, category string) ([]messages.SessionMessage, func(http.ResponseWriter) error, error) {
	return loadMessagesOfType(r, category, "")
}

// An empty messageType selects every message. Other types remain queued.
func loadMessagesOfType(r *http.Request, category, messageType string) ([]messages.SessionMessage, func(http.ResponseWriter) error, error) {
	if r.Method == http.MethodHead {
		return nil, nil, nil
	}
	s, err := session.Get(r, messagesSession.String())
	if err != nil {
		return nil, nil, hs_errors.WithCause(ErrRetrievingContentMessages, err)
	}

	if _, exists := s.Data[category]; !exists {
		return nil, nil, nil
	}
	var contentMessages []messages.SessionMessage
	if err := s.DecodeValue(category, &contentMessages); err != nil {
		return nil, nil, hs_errors.WithCause(ErrRetrievingContentMessages, err)
	}

	var selected, retained []messages.SessionMessage
	for _, msg := range contentMessages {
		if messageType == "" || msg.Type == messageType {
			selected = append(selected, msg)
		} else {
			retained = append(retained, msg)
		}
	}
	if len(selected) == 0 {
		return nil, nil, nil
	}

	acknowledged := false
	acknowledge := func(w http.ResponseWriter) error {
		if acknowledged {
			return nil
		}
		var current []messages.SessionMessage
		if err := s.DecodeValue(category, &current); err != nil {
			return hs_errors.WithCause(ErrDeletingContentMessages, err)
		}
		if len(current) < len(contentMessages) || !slices.Equal(current[:len(contentMessages)], contentMessages) {
			return hs_errors.WithCause(ErrDeletingContentMessages, errors.New("messages changed before acknowledgment"))
		}
		stored := s.Data[category]
		remaining := append(slices.Clone(retained), current[len(contentMessages):]...)
		delete(s.Data, category)
		if len(remaining) > 0 {
			s.Data[category] = remaining
		}

		var err error
		if len(s.Data) == 0 {
			err = s.End(w, r)
		} else {
			err = s.Save(w, r)
		}
		if err != nil {
			s.Data[category] = stored
			return hs_errors.WithCause(ErrDeletingContentMessages, err)
		}
		acknowledged = true
		return nil
	}
	return selected, acknowledge, nil
}
