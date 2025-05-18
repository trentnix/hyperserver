// notifications.go defines a Notification and related methods
package messages

import "net/http"

type (
	Notification struct {
		SessionMessage
	}

	NotificationType string
)

const (
	NotificationTypeSuccess NotificationType = "success"
	NotificationTypeError   NotificationType = "error"

	categoryNotification string = "hs-message-category-notification"
)

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

// GetNotifications returns the slice of Notification instances that have been saved to
// a session. The messages (and the session they are contained in) are then deleted,
// meaning that this function is not idempotent.
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
