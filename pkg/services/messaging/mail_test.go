package messaging

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net"
	mailaddr "net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

type senderFunc func(context.Context, MailMessage) error

func (f senderFunc) Send(ctx context.Context, message MailMessage) error {
	return f(ctx, message)
}

func TestMailRejectsUnsafeHeaders(t *testing.T) {
	for _, field := range []string{"from", "to", "subject"} {
		for _, value := range []string{"person@example.invalid\r\nBcc: other@example.invalid", "value\nInjected: yes", "value\rInjected: yes", "value\x00", "value\x7f", "value\xff"} {
			t.Run(field+"/"+strconv.Quote(value), func(t *testing.T) {
				calls := 0
				client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
					calls++
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				message := client.Compose().To("to@example.invalid").Subject("Test").Body("Hello")
				switch field {
				case "from":
					message.From(value)
				case "to":
					message.To(value)
				case "subject":
					message.Subject(value)
				}
				if err := message.Send(context.Background()); err == nil || calls != 0 {
					t.Fatalf("unsafe header: error=%v, sender calls=%d", err, calls)
				}
			})
		}
	}
}

func TestMailHonorsCanceledContext(t *testing.T) {
	called := false
	client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.Compose().To("to@example.invalid").Body("Hello").Send(ctx)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled send: error=%v, sender called=%v", err, called)
	}
}

func TestMailRejectsInvalidAddresses(t *testing.T) {
	for _, value := range []string{"not-an-address", "a@example.invalid, b@example.invalid", "=?UTF-8?Q?name=0D=0ABcc=3A?= <a@example.invalid>"} {
		for _, field := range []string{"from", "to"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
					t.Error("sender called with invalid address")
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				message := client.Compose().To("to@example.invalid").Body("Hello")
				if field == "from" {
					message.From(value)
				} else {
					message.To(value)
				}
				if err := message.Send(context.Background()); err == nil {
					t.Fatal("accepted invalid address")
				}
			})
		}
	}
}

func TestSMTPTimeoutConfiguration(t *testing.T) {
	for _, timeout := range []time.Duration{0, 5 * time.Second, -time.Second} {
		client, err := NewMailClient(&config.Config{Mail: config.MailConfig{Timeout: timeout}})
		if timeout < 0 {
			if err == nil {
				t.Fatal("accepted negative timeout")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := timeout
		if want == 0 {
			want = 30 * time.Second
		}
		if got := client.sender.(*smtpSender).config.Timeout; got != want {
			t.Fatalf("timeout=%v, want %v", got, want)
		}
	}
}

// Each peer runs on a pipe. The fallback deadline also bounds a broken client.
func smtpTestClient(t *testing.T, serve func(net.Conn) error) (*MailClient, <-chan struct{}) {
	t.Helper()
	client, err := NewMailClient(&config.Config{Mail: config.MailConfig{
		Hostname: "localhost", Port: 587, User: "test", Password: "test",
		FromAddress: "Sender <from@example.invalid>", Timeout: 2 * time.Second,
	}})
	if err != nil {
		t.Fatal(err)
	}
	local, peer := net.Pipe()
	if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer peer.Close()
		if err := serve(peer); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("SMTP peer: %v", err)
		}
	}()
	t.Cleanup(func() { local.Close(); peer.Close(); <-done })
	client.sender.(*smtpSender).dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "localhost:587" {
			t.Errorf("dial %s %s", network, address)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return local, nil
	}
	return client, done
}

type smtpTestExchange struct {
	from, to     string
	failStage    string
	failCode     int
	stallStage   string
	onStall      func()
	startTLS     bool
	skipGreeting bool
	message      string
}

func (s *smtpTestExchange) serve(conn net.Conn) error {
	peer := textproto.NewConn(conn)
	from, to := s.from, s.to
	if from == "" {
		from = "from@example.invalid"
	}
	if to == "" {
		to = "to@example.invalid"
	}
	steps := []struct{ stage, command, response string }{
		{"greeting", "", "220 localhost ready"},
		{"hello", "EHLO localhost", "250-localhost\r\n250 AUTH PLAIN"},
		{"auth", "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00test\x00test")), "235 authenticated"},
		{"mail", "MAIL FROM:<" + from + ">", "250 sender accepted"},
		{"recipient", "RCPT TO:<" + to + ">", "250 recipient accepted"},
		{"data", "DATA", "354 send message"},
		{"body", "", "250 queued"},
		{"quit", "QUIT", "221 closing"},
	}
	if s.startTLS {
		steps = steps[:2]
		steps[1].response = "250-localhost\r\n250 STARTTLS"
		steps = append(steps, struct{ stage, command, response string }{"tls", "STARTTLS", "220 begin TLS"})
	}
	if s.skipGreeting {
		steps = steps[1:]
	}
	for _, step := range steps {
		if step.command != "" {
			line, err := peer.ReadLine()
			if err != nil {
				return err
			}
			if line != step.command {
				return fmt.Errorf("command=%q, want %q", line, step.command)
			}
		}
		if step.stage == "body" {
			body, err := peer.ReadDotBytes()
			if err != nil {
				return err
			}
			s.message = string(body)
		}
		if s.stallStage == step.stage {
			if s.onStall != nil {
				s.onStall()
			}
			_, err := io.Copy(io.Discard, conn)
			return err
		}
		if s.failStage == step.stage {
			if err := peer.PrintfLine("%d rejected", s.failCode); err != nil {
				return err
			}
			// Auth attempts QUIT after an error. Other failures close immediately.
			if step.stage == "auth" {
				if _, err := peer.ReadLine(); err != nil {
					return err
				}
				return peer.PrintfLine("221 closing")
			}
			return nil
		}
		if err := peer.PrintfLine("%s", step.response); err != nil {
			return err
		}
	}
	if s.startTLS {
		if s.onStall != nil {
			s.onStall()
		}
		_, err := io.Copy(io.Discard, conn)
		return err
	}
	return nil
}

func TestSMTPDeliveryAndRejections(t *testing.T) {
	for _, tc := range []struct {
		stage string
		code  int
	}{
		{"", 0}, {"auth", 535}, {"mail", 550}, {"recipient", 550}, {"data", 554}, {"body", 554}, {"quit", 421},
	} {
		t.Run("reject="+tc.stage, func(t *testing.T) {
			exchange := &smtpTestExchange{failStage: tc.stage, failCode: tc.code}
			client, done := smtpTestClient(t, exchange.serve)
			err := client.Compose().To("Recipient <to@example.invalid>").Subject("Welcome, José").Body("<p>Hello &amp; welcome</p>").Send(context.Background())
			<-done
			if tc.code != 0 {
				var smtpError *textproto.Error
				if !errors.As(err, &smtpError) || smtpError.Code != tc.code {
					t.Fatalf("error=%v, want SMTP %d", err, tc.code)
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
			subject, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
			if err != nil || subject != "Welcome, José" {
				t.Fatalf("subject=%q, error=%v", subject, err)
			}
			for field, want := range map[string]string{"From": "from@example.invalid", "To": "to@example.invalid"} {
				address, err := mailaddr.ParseAddress(message.Header.Get(field))
				if err != nil || address.Address != want {
					t.Fatalf("%s=%v, error=%v", field, address, err)
				}
			}
			body, err := io.ReadAll(message.Body)
			if err != nil || string(body) != "<p>Hello &amp; welcome</p>\n" {
				t.Fatalf("body=%q, error=%v", body, err)
			}
		})
	}
}

func TestSMTPPreservesQuotedMailboxes(t *testing.T) {
	for _, mailbox := range []string{
		`"user name"@example.invalid`,
		`"user\"name"@example.invalid`,
		`"user\\name"@example.invalid`,
		`"user@name"@example.invalid`,
		`"user>name"@example.invalid`,
	} {
		for _, field := range []string{"from", "to"} {
			t.Run(field+"/"+mailbox, func(t *testing.T) {
				exchange := &smtpTestExchange{}
				if field == "from" {
					exchange.from = mailbox
				} else {
					exchange.to = mailbox
				}
				client, done := smtpTestClient(t, exchange.serve)
				message := client.Compose().To("to@example.invalid").Body("Hello")
				if field == "from" {
					message.From("Sender <" + mailbox + ">")
				} else {
					message.To("Recipient <" + mailbox + ">")
				}
				err := message.Send(context.Background())
				<-done
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSMTPInterruptsBlockedOperations(t *testing.T) {
	for _, stage := range []string{"greeting", "hello", "auth", "mail", "recipient", "data", "body", "quit", "starttls", "implicit tls"} {
		for _, mode := range []string{"timeout", "request deadline", "cancellation"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				exchange := &smtpTestExchange{stallStage: stage, startTLS: stage == "starttls"}
				if mode == "cancellation" {
					exchange.onStall = cancel
				}
				serve := exchange.serve
				if stage == "implicit tls" {
					serve = func(conn net.Conn) error {
						var firstByte [1]byte
						if _, err := io.ReadFull(conn, firstByte[:]); err != nil {
							return err
						}
						if exchange.onStall != nil {
							exchange.onStall()
						}
						_, err := io.Copy(io.Discard, conn)
						return err
					}
				}
				client, _ := smtpTestClient(t, serve)
				sender := client.sender.(*smtpSender)
				if stage == "implicit tls" {
					sender.config.Port = 465
					dial := sender.dialContext
					sender.dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
						return dial(ctx, network, "localhost:587")
					}
				}
				want := context.DeadlineExceeded
				switch mode {
				case "timeout":
					sender.config.Timeout = 30 * time.Millisecond
				case "request deadline":
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, 30*time.Millisecond)
					defer deadlineCancel()
				case "cancellation":
					want = context.Canceled
				}
				start := time.Now()
				err := client.Compose().To("to@example.invalid").Body("Hello").Send(ctx)
				if !errors.Is(err, want) || time.Since(start) > time.Second {
					t.Fatalf("error=%v, elapsed=%v, want %v", err, time.Since(start), want)
				}
			})
		}
	}
}

func TestSMTPDialHonorsTimeout(t *testing.T) {
	client, err := NewMailClient(&config.Config{Mail: config.MailConfig{
		Hostname: "localhost", Port: 587, User: "test", Password: "test", FromAddress: "from@example.invalid", Timeout: 30 * time.Millisecond,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client.sender.(*smtpSender).dialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	err = client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dial error=%v", err)
	}
}

func TestSMTPRejectsUntrustedTLS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []uint16{587, 465, 2465} {
		t.Run(strconv.Itoa(int(port)), func(t *testing.T) {
			client, done := smtpTestClient(t, func(conn net.Conn) error {
				if port == 587 {
					peer := textproto.NewConn(conn)
					if err := peer.PrintfLine("220 localhost ready"); err != nil {
						return err
					}
					if _, err := peer.ReadLine(); err != nil {
						return err
					}
					if err := peer.PrintfLine("250-localhost\r\n250 STARTTLS"); err != nil {
						return err
					}
					if command, err := peer.ReadLine(); err != nil || command != "STARTTLS" {
						return fmt.Errorf("expected STARTTLS, got %q: %v", command, err)
					}
					if err := peer.PrintfLine("220 begin TLS"); err != nil {
						return err
					}
				}
				// TLS 1.2 keeps this certificate-rejection exchange on one flight.
				// TLS 1.3 can deadlock alert and handshake writes on an unbuffered pipe.
				server := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
				if err := server.Handshake(); err == nil {
					return errors.New("client accepted an untrusted certificate")
				}
				return nil
			})
			sender := client.sender.(*smtpSender)
			sender.config.Port = port
			dial := sender.dialContext
			sender.dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dial(ctx, network, "localhost:587")
			}
			err := client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
			<-done
			var certificateError x509.UnknownAuthorityError
			if !errors.As(err, &certificateError) {
				t.Fatalf("TLS error=%v, want untrusted certificate", err)
			}
		})
	}
}

func TestSMTPEncryptedDelivery(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(trusted)

	for _, port := range []uint16{587, 465, 2465} {
		for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
			t.Run(fmt.Sprintf("port=%d/TLS=%x", port, version), func(t *testing.T) {
				// TCP buffering permits complete TLS exchanges, including close alerts.
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { listener.Close() })
				exchange := &smtpTestExchange{skipGreeting: port == 587}
				done := make(chan error, 1)
				go func() {
					done <- func() error {
						conn, err := listener.Accept()
						if err != nil {
							return err
						}
						defer conn.Close()
						if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
							return err
						}
						if port == 587 {
							peer := textproto.NewConn(conn)
							if err := peer.PrintfLine("220 localhost ready"); err != nil {
								return err
							}
							if command, err := peer.ReadLine(); err != nil || command != "EHLO localhost" {
								return fmt.Errorf("expected EHLO, got %q: %v", command, err)
							}
							// AUTH is advertised only after TLS and a fresh EHLO.
							if err := peer.PrintfLine("250-localhost\r\n250 STARTTLS"); err != nil {
								return err
							}
							if command, err := peer.ReadLine(); err != nil || command != "STARTTLS" {
								return fmt.Errorf("expected STARTTLS, got %q: %v", command, err)
							}
							if err := peer.PrintfLine("220 begin TLS"); err != nil {
								return err
							}
						}
						server := tls.Server(conn, &tls.Config{
							Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
							MinVersion:   version, MaxVersion: version,
						})
						defer server.Close()
						if err := server.Handshake(); err != nil {
							return err
						}
						if state := server.ConnectionState(); state.Version != version || state.ServerName != "localhost" {
							return fmt.Errorf("unexpected TLS state: version=%x, server name=%q", state.Version, state.ServerName)
						}
						return exchange.serve(server)
					}()
				}()
				client, err := NewMailClient(&config.Config{Mail: config.MailConfig{
					Hostname: "localhost", Port: port, User: "test", Password: "test",
					FromAddress: "Sender <from@example.invalid>", Timeout: 3 * time.Second,
				}})
				if err != nil {
					t.Fatal(err)
				}
				sender := client.sender.(*smtpSender)
				sender.rootCAs = roots
				sender.dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
					if address != net.JoinHostPort("localhost", strconv.Itoa(int(port))) {
						t.Errorf("unexpected dial address %q", address)
					}
					return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
				}
				sendErr := client.Compose().To("Recipient <to@example.invalid>").Subject("Encrypted delivery").Body("<p>Hello &amp; welcome</p>").Send(context.Background())
				listener.Close() // Unblock Accept if the client failed before dialing.
				if peerErr := <-done; peerErr != nil {
					t.Errorf("SMTP peer: %v", peerErr)
				}
				if sendErr != nil {
					t.Fatal(sendErr)
				}
				message, err := mailaddr.ReadMessage(strings.NewReader(exchange.message))
				if err != nil {
					t.Fatal(err)
				}
				for field, want := range map[string]string{
					"From": `"Sender" <from@example.invalid>`, "To": `"Recipient" <to@example.invalid>`,
					"Subject": "Encrypted delivery", "Content-Type": `text/html; charset="UTF-8"`,
				} {
					if got := message.Header.Get(field); got != want {
						t.Errorf("%s=%q, want %q", field, got, want)
					}
				}
				body, err := io.ReadAll(message.Body)
				if err != nil || string(body) != "<p>Hello &amp; welcome</p>\n" {
					t.Fatalf("body=%q, error=%v", body, err)
				}
			})
		}
	}
}

func TestSMTPRefusesRemotePlaintextAuthentication(t *testing.T) {
	client, done := smtpTestClient(t, func(conn net.Conn) error {
		peer := textproto.NewConn(conn)
		if err := peer.PrintfLine("220 remote ready"); err != nil {
			return err
		}
		if _, err := peer.ReadLine(); err != nil {
			return err
		}
		if err := peer.PrintfLine("250-remote\r\n250 AUTH PLAIN"); err != nil {
			return err
		}
		command, err := peer.ReadLine()
		if err != nil || command != "QUIT" {
			return fmt.Errorf("expected QUIT without credentials, got %q: %v", command, err)
		}
		return peer.PrintfLine("221 closing")
	})
	sender := client.sender.(*smtpSender)
	sender.config.Hostname = "smtp.example.invalid"
	dial := sender.dialContext
	sender.dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dial(ctx, network, "localhost:587")
	}
	err := client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
	<-done
	if err == nil || !strings.Contains(err.Error(), "unencrypted connection") {
		t.Fatalf("error=%v, want refusal to send credentials", err)
	}
}

func TestSMTPGreetingHonorsDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// A fallback deadline keeps the regression bounded even before the fix.
		conn.SetDeadline(time.Now().Add(time.Second))
		var b [1]byte
		conn.Read(b[:])
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	cfg := &config.Config{Mail: config.MailConfig{
		Hostname: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port),
		User: "test", Password: "test", FromAddress: "from@example.invalid",
	}}
	client, err := NewMailClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = client.Compose().To("to@example.invalid").Body("Hello").Send(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("stalled greeting: error=%v, elapsed=%v", err, time.Since(started))
	}
}

func TestMailClientUsesInjectedSender(t *testing.T) {
	for _, from := range []string{"", "override@example.invalid"} {
		t.Run("from="+from, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("delivery failed")
			calls := 0
			client, err := NewMailClientWithSender("default@example.invalid", senderFunc(func(gotCtx context.Context, message MailMessage) error {
				calls++
				wantFrom := from
				if wantFrom == "" {
					wantFrom = "default@example.invalid"
				}
				want := MailMessage{From: wantFrom, To: "recipient@example.invalid", Subject: "Test", Body: "<p>Hello</p>"}
				if message != want || gotCtx != ctx {
					t.Errorf("sender received message %+v and context %v, want %+v and original context", message, gotCtx, want)
				}
				return wantErr
			}))
			if err != nil {
				t.Fatal(err)
			}
			message := client.Compose().To("recipient@example.invalid").Subject("Test").Body("<p>Hello</p>")
			if from != "" {
				message.From(from)
			}
			if err := message.Send(ctx); !errors.Is(err, wantErr) {
				t.Fatalf("Send error = %v, want %v", err, wantErr)
			}
			if calls != 1 {
				t.Fatalf("sender calls = %d, want 1", calls)
			}
		})
	}
}

func TestMailClientRejectsIncompleteMessages(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, body, want string
	}{
		{"from", "", "to@example.invalid", "Hello", "email cannot be sent without a from address"},
		{"to", "from@example.invalid", "", "Hello", "email cannot be sent without a to address"},
		{"body", "from@example.invalid", "to@example.invalid", "", "email cannot be sent without a body to render"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewMailClientWithSender(tc.from, senderFunc(func(context.Context, MailMessage) error {
				t.Fatal("sender called for an incomplete message")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Compose().To(tc.to).Body(tc.body).Send(context.Background()); err == nil || err.Error() != tc.want {
				t.Fatalf("Send error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMailClientRequiresDependencies(t *testing.T) {
	if _, err := NewMailClient(nil); err == nil {
		t.Fatal("accepted nil configuration")
	}
	if _, err := NewMailClientWithSender("from@example.invalid", nil); err == nil {
		t.Fatal("accepted nil sender")
	}
}

func TestSMTPUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		omit func(*config.MailConfig)
	}{
		{"all settings", func(c *config.MailConfig) { *c = config.MailConfig{FromAddress: c.FromAddress} }},
		{"hostname", func(c *config.MailConfig) { c.Hostname = "" }},
		{"port", func(c *config.MailConfig) { c.Port = 0 }},
		{"user", func(c *config.MailConfig) { c.User = "" }},
		{"password", func(c *config.MailConfig) { c.Password = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Mail: config.MailConfig{
				Hostname: "smtp.example.invalid", Port: 587,
				User: "test-user", Password: "test-password", FromAddress: "from@example.invalid",
			}}
			tc.omit(&cfg.Mail)
			client, err := NewMailClient(cfg)
			if err != nil {
				t.Fatalf("unconfigured mail must not prevent client construction: %v", err)
			}
			err = client.Compose().To("to@example.invalid").Subject("Test").Body("Hello").Send(context.Background())
			if !errors.Is(err, ErrMailUnavailable) {
				t.Fatalf("Send error = %v, want ErrMailUnavailable", err)
			}
		})
	}
}

func TestMailClientReportsSenderOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"accepted", nil},
		{"unavailable", ErrMailUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
				calls++
				return tc.err
			}))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
			if !errors.Is(err, tc.err) || calls != 1 {
				t.Fatalf("Send = %v, calls = %d, want %v and one call", err, calls, tc.err)
			}
		})
	}
}
