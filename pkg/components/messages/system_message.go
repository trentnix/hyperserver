package messages

type (
	// SystemMessage is a message intended for the browser console.
	SystemMessage struct {
		SessionMessage
	}

	// SystemMessageType selects a browser console logging method.
	SystemMessageType string
)

const (
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

// NewSystemMessage creates a new instance of a SystemMessage struct
func NewSystemMessage(message string, messageType SystemMessageType) SystemMessage {
	return SystemMessage{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}
