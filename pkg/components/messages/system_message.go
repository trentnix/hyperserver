// system_message.go defines a SystemMessage and related methods
package messages

import (
	"net/http"
)

type (
	SystemMessage struct {
		SessionMessage
	}

	SystemMessageType string
)

const (
	categorySystemMessage string = "hs-message-system-content-message"

	SystemMessageTypeDebug   SystemMessageType = "debug"
	SystemMessageTypeLog     SystemMessageType = "log"
	SystemMessageTypeInfo    SystemMessageType = "info"
	SystemMessageTypeWarning SystemMessageType = "warning"
	SystemMessageTypeError   SystemMessageType = "error"
)

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

// NewSystemMessage creates a new instance of a SystemMessage struct
func NewSystemMessage(message string, messageType SystemMessageType) SystemMessage {
	return SystemMessage{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}

// AddErrorMessage adds an error ContentMessage to a session
func AddSystemErrorMessage(w http.ResponseWriter, r *http.Request, message string) error {
	return addSystemMessage(w, r, NewSystemMessage(message, SystemMessageTypeError))
}

// GetSystemMessages returns the slice of SystemMessage instances that have been saved to
// a session. The messages (and the session they are contained in) are then deleted,
// meaning that this function is not idempotent.
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
