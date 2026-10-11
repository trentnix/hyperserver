// Package messages defines message values used by forms and templates.
package messages

// SessionMessage is the text and type shared by message categories.
// Its JSON fields also define the stored representation used by flash messages.
type SessionMessage struct {
	Message string `json:"message"`
	Type    string `json:"messageType"`
}
