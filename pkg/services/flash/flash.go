package flash

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

// AuthMessages is the flash-message category used by authentication forms.
const AuthMessages = "hs_auth"

const sessionFlashMessagePrefix = "hs-message-category-flash-message"

// AddMessage stores an informational message in the specified session category.
func AddMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, messages.NewMessage(message, messages.MessageTypeDefault), category)
}

// AddErrorMessage stores an error message in the specified session category.
func AddErrorMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, messages.NewMessage(message, messages.MessageTypeError), category)
}

// AddSuccessMessage stores a success message in the specified session category.
func AddSuccessMessage(w http.ResponseWriter, r *http.Request, message string, category string) error {
	return addFlashMessage(w, r, messages.NewMessage(message, messages.MessageTypeSuccess), category)
}

// GetMessages retrieves a category and persists its removal, ending an empty session.
// A failed removal returns no messages and leaves the request-local category intact.
func GetMessages(w http.ResponseWriter, r *http.Request, category string) ([]messages.Message, error) {
	if category == "" {
		return nil, NewErrMessageCategoryNotSpecified(nil)
	}

	flashMessages, err := getMessages(w, r, flashMessageCategory(category))
	if err != nil {
		return nil, err
	}

	var msgs []messages.Message
	for _, msg := range flashMessages {
		flashMessage := messages.NewMessage(msg.Message, messages.MessageType(msg.Type))
		msgs = append(msgs, flashMessage)
	}

	return msgs, nil
}

// addFlashMessage serializes the specified message to a session used explicitly for storing
// content messages that can be accessed by handlers. The message is stored by category and can
// be retrieved by category
func addFlashMessage(w http.ResponseWriter, r *http.Request, m messages.Message, c string) error {
	return addMessage(w, r, m.Message, m.Type, flashMessageCategory(c))
}

func flashMessageCategory(category string) string {
	return fmt.Sprintf("%s-%s", sessionFlashMessagePrefix, category)
}
