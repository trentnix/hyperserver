package messages

import (
	"fmt"
	"html/template"
	"net/http"
)

type (
	// SystemMessage is a session-backed message intended for the browser console.
	SystemMessage struct {
		SessionMessage
	}

	// SystemMessageType selects a browser console logging method.
	SystemMessageType string
)

const (
	categorySystemMessage string = "hs-message-system-content-message"

	// SystemMessageTypeDebug selects console.debug.
	SystemMessageTypeDebug SystemMessageType = "debug"
	// SystemMessageTypeLog selects console.log.
	SystemMessageTypeLog SystemMessageType = "log"
	// SystemMessageTypeInfo selects console.info.
	SystemMessageTypeInfo SystemMessageType = "info"
	// SystemMessageTypeWarning selects console.warn.
	SystemMessageTypeWarning SystemMessageType = "warn"
	// SystemMessageTypeError selects console.error.
	SystemMessageTypeError SystemMessageType = "error"
)

// String returns the underlying string value.
func (m SystemMessageType) String() string {
	return string(m)
}

// IsDebug returns true if the specified SystemMessage is a debug message
func (c *SystemMessage) IsDebug() bool {
	return c.Type == SystemMessageTypeDebug.String()
}

// IsLog returns true if the specified SystemMessage is a log message
func (c *SystemMessage) IsLog() bool {
	return c.Type == SystemMessageTypeLog.String()
}

// IsInfo returns true if the specified SystemMessage is an info message
func (c *SystemMessage) IsInfo() bool {
	return c.Type == SystemMessageTypeInfo.String()
}

// IsWarning returns true if the specified SystemMessage is an warning message
func (c *SystemMessage) IsWarning() bool {
	return c.Type == SystemMessageTypeWarning.String()
}

// IsError returns true if the specified SystemMessage is an error message
func (c *SystemMessage) IsError() bool {
	return c.Type == SystemMessageTypeError.String()
}

// ConsoleMessage formats a browser console call as trusted JavaScript.
// It uses Go string quoting, not HTML script-context escaping. Do not pass untrusted text.
func (c *SystemMessage) ConsoleMessage() template.JS {
	var msgType string
	switch SystemMessageType(c.Type) {
	case SystemMessageTypeDebug:
		msgType = "debug"
	case SystemMessageTypeLog:
		msgType = "log"
	case SystemMessageTypeInfo:
		msgType = "info"
	case SystemMessageTypeWarning:
		msgType = "warn"
	case SystemMessageTypeError:
		msgType = "error"
	default:
		msgType = "debug"
	}
	return template.JS(fmt.Sprintf(`console.%s(%q);`, msgType, c.Message))
}

// NewSystemMessage creates a new instance of a SystemMessage struct
func NewSystemMessage(message string, messageType SystemMessageType) SystemMessage {
	return SystemMessage{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}

// AddSystemDebugMessage adds a debug SystemMessage to a session
func AddSystemDebugMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeDebug))
}

// AddSystemLogMessage adds a log SystemMessage to a session
func AddSystemLogMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeLog))
}

// AddSystemInfoMessage adds an info SystemMessage to a session
func AddSystemInfoMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeInfo))
}

// AddSystemWarningMessage stores a warning message for the browser console.
func AddSystemWarningMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeWarning))
}

// AddSystemErrorMessage adds an error SystemMessage to a session
func AddSystemErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeError))
}

// GetSystemMessages retrieves console messages and removes them from the request-local session.
// It ends the session if empty, but otherwise does not save the removal to storage.
func GetSystemMessages(w http.ResponseWriter, r *http.Request) ([]SystemMessage, error) {
	msgs, err := getMessages(w, r, categorySystemMessage)
	if err != nil {
		return nil, err
	}

	var systemMessages []SystemMessage
	for _, msg := range msgs {
		systemMessage := NewSystemMessage(msg.Message, SystemMessageType(msg.Type))
		systemMessages = append(systemMessages, systemMessage)
	}

	return systemMessages, nil
}

// addSystemMessage serializes the specified message to a session used explicitly for storing
// cosystemntent messages that can be accessed by handlers
func addSystemMessage(w http.ResponseWriter, r *http.Request, c SystemMessage) error {
	return addMessage(w, r, c.Message, c.Type, categorySystemMessage)
}
