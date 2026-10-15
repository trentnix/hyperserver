package flash

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

const categoryNotification = "hs-message-category-notification"

// AddErrorNotification adds an error Notification to a session
func AddErrorNotification(w http.ResponseWriter, r *http.Request, message string) error {
	return addNotification(w, r, messages.NewNotification(message, messages.NotificationTypeError))
}

// AddSuccessNotification adds a success Notification to a session
func AddSuccessNotification(w http.ResponseWriter, r *http.Request, message string) error {
	return addNotification(w, r, messages.NewNotification(message, messages.NotificationTypeSuccess))
}

// GetNotifications retrieves notifications and persists their removal, ending an empty session.
// A failed removal returns no notifications and leaves the request-local category intact.
// HEAD returns no notifications and leaves storage unchanged. For rendering, use LoadNotifications.
func GetNotifications(w http.ResponseWriter, r *http.Request) ([]messages.Notification, error) {
	notifications, acknowledge, err := LoadNotifications(r)
	if err != nil {
		return nil, err
	}
	if acknowledge != nil {
		if err := acknowledge(w); err != nil {
			return nil, err
		}
	}
	return notifications, nil
}

// LoadNotifications reads without removing notifications. Call the returned
// acknowledgment after rendering succeeds, before writing response headers.
// It removes only the loaded messages, preserves later additions, and can be
// retried after failure. Successful acknowledgment is a no-op on subsequent calls.
// Both loading and acknowledgment belong to one request, not concurrent goroutines.
// HEAD and missing categories return no notifications or acknowledgment.
func LoadNotifications(r *http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
	return loadNotifications(r, "")
}

// LoadErrorNotifications uses LoadNotifications' acknowledgment rules but selects
// only errors. Other notification types remain queued, including after acknowledgment.
func LoadErrorNotifications(r *http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
	return loadNotifications(r, messages.NotificationTypeError.String())
}

func loadNotifications(r *http.Request, messageType string) ([]messages.Notification, func(http.ResponseWriter) error, error) {
	msgs, acknowledge, err := loadMessagesOfType(r, categoryNotification, messageType)
	if err != nil {
		return nil, nil, err
	}

	var notifications []messages.Notification
	for _, msg := range msgs {
		notification := messages.NewNotification(msg.Message, messages.NotificationType(msg.Type))
		notifications = append(notifications, notification)
	}

	return notifications, acknowledge, nil
}

// addNotification serializes the specified message to a session used explicitly for storing
// notifications that can be accessed by handlers
func addNotification(w http.ResponseWriter, r *http.Request, n messages.Notification) error {
	return addMessage(w, r, n.Message, n.Type, categoryNotification)
}
