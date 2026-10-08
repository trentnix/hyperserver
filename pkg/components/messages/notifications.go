package messages

import "net/http"

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

	categoryNotification string = "hs-message-category-notification"
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

// AddErrorNotification adds an error Notification to a session
func AddErrorNotification(w http.ResponseWriter, r *http.Request, message string) error {
	return addNotification(w, r, NewNotification(message, NotificationTypeError))
}

// AddSuccessNotification adds a success Notification to a session
func AddSuccessNotification(w http.ResponseWriter, r *http.Request, message string) error {
	return addNotification(w, r, NewNotification(message, NotificationTypeSuccess))
}

// GetNotifications retrieves notifications and persists their removal, ending an empty session.
// A failed removal returns no notifications and leaves the request-local category intact.
func GetNotifications(w http.ResponseWriter, r *http.Request) ([]Notification, error) {
	msgs, err := getMessages(w, r, categoryNotification)
	if err != nil {
		return nil, err
	}

	var notifications []Notification
	for _, msg := range msgs {
		notification := NewNotification(msg.Message, NotificationType(msg.Type))
		notifications = append(notifications, notification)
	}

	return notifications, nil
}

// addNotification serializes the specified message to a session used explicitly for storing
// notifications that can be accessed by handlers
func addNotification(w http.ResponseWriter, r *http.Request, n Notification) error {
	return addMessage(w, r, n.Message, n.Type, categoryNotification)
}
