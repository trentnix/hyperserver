package module_site

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
)

func TestConsoleMessagesAreEscapedText(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/html/components/console-messages.html")
	if err != nil {
		t.Fatal(err)
	}
	payload := `</script><script>alert("message")</script>`
	level := `debug"><img src=x onerror=alert(1)>`
	page := &content.Content{SystemMessages: []messages.SystemMessage{
		messages.NewSystemMessage(payload, messages.SystemMessageType(level)),
	}}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "site.component.console_messages", page); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "<script") || strings.Contains(html, "<img") || !strings.Contains(html, template.HTMLEscapeString(payload)) || !strings.Contains(html, template.HTMLEscapeString(level)) {
		t.Fatalf("console message was not escaped as text: %s", html)
	}
}
