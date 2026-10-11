package messages

type (
	// Notification is a success or error message intended for page notifications.
	Notification struct {
		SessionMessage
	}

	// NotificationType identifies a notification's severity.
	NotificationType string
)

const (
	// NotificationTypeSuccess identifies a success notification.
	NotificationTypeSuccess NotificationType = "success"
	// NotificationTypeError identifies an error notification.
	NotificationTypeError NotificationType = "error"
)

// String returns the underlying string value.
func (n NotificationType) String() string {
	return string(n)
}

// IsSuccess returns true if the specified Notification is a success message
func (n *Notification) IsSuccess() bool {
	return n.Type == NotificationTypeSuccess.String()
}

// IsError returns true if the specified Notification is an error message
func (n *Notification) IsError() bool {
	return n.Type == NotificationTypeError.String()
}

// NewNotification creates a new instance of a Notification struct
func NewNotification(message string, notificationType NotificationType) Notification {
	return Notification{
		SessionMessage: SessionMessage{
			Message: message,
			Type:    notificationType.String(),
		},
	}
}
