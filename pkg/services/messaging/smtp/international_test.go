package smtp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	mailaddr "net/mail"
	"net/textproto"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestSMTPAddressPreservesOriginal(t *testing.T) {
	original := mailaddr.Address{Name: "用戶", Address: "Use\u0301r@faß.example"}
	address := original
	converted, err := smtpAddress(&address)
	if err != nil {
		t.Fatal(err)
	}
	if address != original {
		t.Fatalf("original address changed: %+v", address)
	}
	if converted.Name != original.Name || converted.Address != "Use\u0301r@xn--fa-hia.example" {
		t.Fatalf("transport address=%+v", converted)
	}
}

func TestSMTPRejectsInvalidInternationalDomainBeforeDial(t *testing.T) {
	sender, err := NewSender(config.MailConfig{Hostname: "localhost", Port: 587, User: "test", Password: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sender.dialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("invalid domain contacted SMTP")
		return nil, errors.New("unexpected dial")
	}
	for _, field := range []string{"from", "to"} {
		t.Run(field, func(t *testing.T) {
			message := messaging.MailMessage{From: "from@example.invalid", To: "to@example.invalid", Body: "Hello"}
			if field == "from" {
				message.From = "user@\u200d.example"
			} else {
				message.To = "user@\u200d.example"
			}
			if err := sender.Send(t.Context(), message); err == nil || !strings.Contains(err.Error(), "invalid internationalized address domain") {
				t.Fatalf("error=%v, want invalid domain", err)
			}
		})
	}
}

func TestSMTPInternationalDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, wantFrom, wantTo string
		extensions                       []string
		wantError                        string
	}{
		{name: "ASCII", from: "from@example.invalid", to: "to@example.invalid"},
		{name: "8BITMIME only", from: "from@example.invalid", to: "to@example.invalid", extensions: []string{"8BITMIME"}},
		{name: "ASCII with SMTPUTF8", from: "from@example.invalid", to: "to@example.invalid", extensions: []string{"SMTPUTF8", "8BITMIME"}},
		{name: "Unicode domains", from: "From@bücher.example", to: "To@faß.example", wantFrom: "From@xn--bcher-kva.example", wantTo: "To@xn--fa-hia.example"},
		{name: "address literals", from: "from@[127.0.0.1]", to: "to@[127.0.0.2]"},
		{name: "Unicode local parts", from: "Sénder@bücher.example", to: "用戶@faß.example", wantFrom: "Sénder@xn--bcher-kva.example", wantTo: "用戶@xn--fa-hia.example", extensions: []string{"SMTPUTF8", "8BITMIME"}},
		{name: "quoted Unicode local part", from: "from@example.invalid", to: `"用戶 name"@example.invalid`, extensions: []string{"SMTPUTF8", "8BITMIME"}},
		{name: "decomposed Unicode", from: "from@example.invalid", to: "Use\u0301r@example.invalid", extensions: []string{"SMTPUTF8", "8BITMIME"}},
		{name: "unsupported sender", from: "Sénder@example.invalid", to: "to@example.invalid", wantError: "SMTPUTF8"},
		{name: "unsupported recipient", from: "from@example.invalid", to: "用戶@example.invalid", extensions: []string{"8BITMIME"}, wantError: "SMTPUTF8"},
		{name: "missing 8BITMIME", from: "from@example.invalid", to: "用戶@example.invalid", extensions: []string{"SMTPUTF8"}, wantError: "8BITMIME"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantFrom == "" {
				tc.wantFrom = tc.from
			}
			if tc.wantTo == "" {
				tc.wantTo = tc.to
			}
			exchange := &smtpTestExchange{
				from: tc.wantFrom, to: tc.wantTo, extensions: tc.extensions,
				stopAfterHello: tc.wantError != "",
			}
			client, done := smtpTestClient(t, exchange.serve)
			body := "<p>Welcome, José. 歡迎！</p>\r\n" + strings.Repeat("Long line = café ", 100) + "\r\n.\r\ntrailing space \r\n"
			err := client.Compose().From("José <" + tc.from + ">").To("用戶 <" + tc.to + ">").Subject("Welcome, 世界").Body(body).Send(t.Context())
			<-done
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Send error=%v, want %s", err, tc.wantError)
				}
				if exchange.message != "" {
					t.Fatal("submitted an unsupported message")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			message, err := mailaddr.ReadMessage(strings.NewReader(exchange.message))
			if err != nil {
				t.Fatal(err)
			}
			for field, want := range map[string]string{"From": "José <" + tc.wantFrom + ">", "To": "用戶 <" + tc.wantTo + ">"} {
				address, err := mailaddr.ParseAddress(message.Header.Get(field))
				expected, parseErr := mailaddr.ParseAddress(want)
				if err != nil || parseErr != nil || *address != *expected {
					t.Fatalf("%s=%v, want %v (errors: %v, %v)", field, address, expected, err, parseErr)
				}
			}
			subject, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
			if err != nil || subject != "Welcome, 世界" {
				t.Fatalf("subject=%q, error=%v", subject, err)
			}
			if message.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
				t.Fatal("missing quoted-printable encoding header")
			}
			wireBody, err := io.ReadAll(message.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(wireBody), "\n") {
				if containsNonASCII(line) || len(line) > 76 {
					t.Fatalf("body is not bounded 7-bit MIME: %q", line)
				}
			}
			decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(wireBody)))
			// ReadDotBytes normalizes CRLF before MIME decoding.
			if err != nil || string(decoded) != strings.ReplaceAll(body, "\r\n", "\n") {
				t.Fatalf("body round trip failed: %q, error=%v", decoded, err)
			}
			if !containsNonASCII(tc.wantFrom + tc.wantTo) {
				if containsNonASCII(exchange.message) {
					t.Fatal("ASCII mailboxes require no raw UTF-8 in message headers or body")
				}
			}
		})
	}
}

func TestSMTPInternationalDeliveryRejections(t *testing.T) {
	for _, stage := range []string{"mail", "recipient", "data", "body", "quit"} {
		t.Run(stage, func(t *testing.T) {
			exchange := &smtpTestExchange{
				to: "用戶@example.invalid", extensions: []string{"SMTPUTF8", "8BITMIME"},
				failStage: stage, failCode: 550,
			}
			client, done := smtpTestClient(t, exchange.serve)
			err := client.Compose().To(exchange.to).Body("<p>歡迎！</p>").Send(t.Context())
			<-done
			var smtpError *textproto.Error
			if !errors.As(err, &smtpError) || smtpError.Code != 550 {
				t.Fatalf("error=%v, want SMTP 550", err)
			}
		})
	}
}
