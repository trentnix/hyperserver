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
func GetNotifications(w http.ResponseWriter, r *http.Request) ([]messages.Notification, error) {
	msgs, err := getMessages(w, r, categoryNotification)
	if err != nil {
		return nil, err
	}

	var notifications []messages.Notification
	for _, msg := range msgs {
		notification := messages.NewNotification(msg.Message, messages.NotificationType(msg.Type))
		notifications = append(notifications, notification)
	}

	return notifications, nil
}

// addNotification serializes the specified message to a session used explicitly for storing
// notifications that can be accessed by handlers
func addNotification(w http.ResponseWriter, r *http.Request, n messages.Notification) error {
	return addMessage(w, r, n.Message, n.Type, categoryNotification)
}
