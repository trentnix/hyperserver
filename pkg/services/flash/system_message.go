package flash

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

const categorySystemMessage = "hs-message-system-content-message"

// AddSystemDebugMessage adds a debug SystemMessage to a session
func AddSystemDebugMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, messages.NewSystemMessage(message, messages.SystemMessageTypeDebug))
}

// AddSystemLogMessage adds a log SystemMessage to a session
func AddSystemLogMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, messages.NewSystemMessage(message, messages.SystemMessageTypeLog))
}

// AddSystemInfoMessage adds an info SystemMessage to a session
func AddSystemInfoMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, messages.NewSystemMessage(message, messages.SystemMessageTypeInfo))
}

// AddSystemWarningMessage stores a warning message for the browser console.
func AddSystemWarningMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, messages.NewSystemMessage(message, messages.SystemMessageTypeWarning))
}

// AddSystemErrorMessage adds an error SystemMessage to a session
func AddSystemErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, messages.NewSystemMessage(message, messages.SystemMessageTypeError))
}

// GetSystemMessages retrieves console messages and persists their removal, ending an empty session.
// A failed removal returns no messages and leaves the request-local category intact.
func GetSystemMessages(w http.ResponseWriter, r *http.Request) ([]messages.SystemMessage, error) {
	msgs, err := getMessages(w, r, categorySystemMessage)
	if err != nil {
		return nil, err
	}

	var systemMessages []messages.SystemMessage
	for _, msg := range msgs {
		systemMessage := messages.NewSystemMessage(msg.Message, messages.SystemMessageType(msg.Type))
		systemMessages = append(systemMessages, systemMessage)
	}

	return systemMessages, nil
}

// addSystemMessage stores a browser console message in the messages session.
func addSystemMessage(w http.ResponseWriter, r *http.Request, c messages.SystemMessage) error {
	return addMessage(w, r, c.Message, c.Type, categorySystemMessage)
}
