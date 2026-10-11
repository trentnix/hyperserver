package messages

type (
	// Message is a categorized message for forms and pages.
	Message struct {
		SessionMessage
	}

	// MessageType identifies an informational, success, or error flash message.
	MessageType string
)

const (
	// MessageTypeDefault identifies an informational flash message.
	MessageTypeDefault MessageType = "default"
	// MessageTypeSuccess identifies a successful operation.
	MessageTypeSuccess MessageType = "success"
	// MessageTypeError identifies a failed operation.
	MessageTypeError MessageType = "error"
)

// String returns the underlying string value.
func (m MessageType) String() string {
	return string(m)
}

// IsDefault returns true if the specified ContentMessage is a default message
func (c *Message) IsDefault() bool {
	return c.Type == MessageTypeDefault.String()
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *Message) IsSuccess() bool {
	return c.Type == MessageTypeSuccess.String()
}

// IsError returns true if the specified ContentMessage is an error message
func (c *Message) IsError() bool {
	return c.Type == MessageTypeError.String()
}

// NewMessage creates a new instance of a Message struct
func NewMessage(message string, messageType MessageType) Message {
	return Message{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    messageType.String(),
		},
	}
}
