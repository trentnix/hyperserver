package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestRunDrainsRequestsBeforeClosingPool(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			testRunDrainsRequestsBeforeClosingPool(t, scheme)
		})
	}
}

func testRunDrainsRequestsBeforeClosingPool(t *testing.T, scheme string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shutdown.db")
	yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	mode := "shutdown"
	if scheme == "https" {
		mode = "shutdown-tls"
	}
	output, err := runStartupProcess(t, yaml, mode, "HYPERSERVER_DATABASE_CONNECTION="+path)
	if err != nil || !strings.Contains(string(output), "shutdown ordering passed") {
		t.Fatalf("application shutdown: error = %v, output = %s", err, output)
	}

	// Reopen independently to verify the request committed before its pool closed.
	db, err := sqlx.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := user.GetUserByEmail(db, "shutdown@example.invalid"); err != nil {
		t.Fatal("shutdown lost the in-flight request's write:", err)
	}
}

// checkRunShutdown runs in a subprocess to isolate registries, stdout, and cwd.
func checkRunShutdown(t *testing.T, secure bool) {
	t.Helper()
	s := server.NewApplicationServer()
	s.Config.HTTP.Port = 0
	scheme := "http"
	transport := &http.Transport{}
	if secure {
		cfg, roots := testTLSFiles(t)
		s.Config.HTTP.TLS = cfg.TLS
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
		scheme = "https"
	}
	defer transport.CloseIdleConnections()
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	var activeSession *session.Session
	s.Web.HandleFunc("POST /test-shutdown", func(w http.ResponseWriter, r *http.Request) {
		if (r.TLS != nil) != secure {
			t.Error("request TLS state does not match the listener configuration")
		}
		close(started)
		<-release
		account := &user.User{Email: "shutdown@example.invalid"}
		if err := account.Create(r.Context(), s.Database); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var err error
		activeSession, err = session.New(r, "auth-user-session")
		if err == nil {
			err = activeSession.Save(w, r)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Read run's actual listening address rather than reserving and releasing a port.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	stdout := os.Stdout
	os.Stdout = writer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- run(ctx, s)
	}()
	defer func() {
		cancel()
		releaseRequest()
		awaitSignal(t, finished)
		writer.Close()
		os.Stdout = stdout
	}()
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			if value, ok := strings.CutPrefix(scanner.Text(), "Starting server at "); ok {
				address <- value
				return
			}
		}
	}()
	var listeningAddress string
	select {
	case listeningAddress = <-address:
	case err := <-done:
		t.Fatalf("application stopped before listening: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("application did not start listening")
	}

	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		res, err := client.Post(scheme+"://"+listeningAddress+"/test-shutdown", "text/plain", nil)
		if err == nil {
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			err = readErr
			if res.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("in-flight request returned %d: %s", res.StatusCode, body)
			}
		}
		response <- err
	}()
	awaitSignal(t, started)
	cancel()

	// Wait for shutdown to stop accepting connections while the request is held.
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", listeningAddress, 100*time.Millisecond)
		if err != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("cancellation did not close the listener")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("run returned before the active request finished: %v", err)
	default:
	}
	if err := s.Database.Ping(); err != nil {
		t.Fatal("run closed the pool before its active request finished:", err)
	}
	releaseRequest()
	awaitResult(t, response)
	awaitResult(t, done)
	if err := s.Database.Ping(); err == nil {
		t.Fatal("run returned without closing the application's pool")
	}
	if activeSession == nil {
		t.Fatal("in-flight request did not save its session")
	}
	if err := activeSession.Save(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
		t.Fatal("run returned without closing the session pool")
	}
	fmt.Fprintln(stdout, "shutdown ordering passed")
}

func TestStoppingBorrowerDrainsRequestsAndPreservesPool(t *testing.T) {
	db := accountSetupTestDB(t)
	owner := &server.ApplicationServer{Database: db}
	if err := user.PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repository, err := models.NewContactRepository(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closing := &observedListener{Listener: listener, closed: make(chan struct{})}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRequest()
	s := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		submission := &models.ContactSubmission{Email: "person@example.invalid", Message: "Still running"}
		if err := repository.Create(r.Context(), submission); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, s, closing) }()
	t.Cleanup(func() {
		cancel()
		releaseRequest()
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})

	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		res, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				err = errors.New("in-flight request failed to save its contact submission")
			}
		}
		response <- err
	}()
	awaitSignal(t, started)
	cancel()
	awaitSignal(t, closing.closed)
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before the active request finished: %v", err)
	default:
	}

	// The account consumer still uses the same pool while the site is stopping.
	account := &user.User{Email: "before@example.invalid"}
	if err := account.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	releaseRequest()
	awaitResult(t, response)
	awaitResult(t, done)
	account = &user.User{Email: "after@example.invalid"}
	if err := account.Create(context.Background(), db); err != nil {
		t.Fatal("stopping the site closed the account consumer's pool:", err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM hyperserver_contact_submission`); err != nil || count != 1 {
		t.Fatalf("active request did not persist before shutdown: count = %d, error = %v", count, err)
	}
	if err := owner.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err == nil {
		t.Fatal("application shutdown left its pool open")
	}
}

func TestRunClosesPoolOnInitializationFailure(t *testing.T) {
	for _, failure := range []string{"listen address", "TLS files", "session setup", "account setup"} {
		t.Run(failure, func(t *testing.T) {
			db := accountSetupTestDB(t)
			s := &server.ApplicationServer{Database: db, Config: &config.Config{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "listen address":
				s.Config.HTTP.ListenHost = "0.0.0.0"
			case "TLS files":
				s.Config.HTTP.TLS.Enabled = true
				s.Config.HTTP.TLS.Certificate = "missing-certificate.pem"
				s.Config.HTTP.TLS.Key = "missing-key.pem"
			case "account setup":
				s.Config.Auth.Enabled = true
				if _, err := db.Exec("CREATE TABLE usertoken (incompatible TEXT)"); err != nil {
					t.Fatal(err)
				}
			case "session setup":
				cancel()
			}
			if err := run(ctx, s); err == nil {
				t.Fatal("invalid initialization succeeded")
			}
			if err := db.Ping(); err == nil {
				t.Fatal("initialization failure leaked the pool")
			}
		})
	}
}

func TestServeHTTPReturnsListenerFailure(t *testing.T) {
	want := errors.New("accept failed")
	listener := &failedListener{err: want}
	if err := serveHTTP(context.Background(), &http.Server{}, listener); !errors.Is(err, want) {
		t.Fatalf("serve error = %v, want %v", err, want)
	}
}

type failedListener struct {
	err error
}

func (l *failedListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *failedListener) Close() error              { return nil }
func (l *failedListener) Addr() net.Addr            { return &net.TCPAddr{} }

type observedListener struct {
	net.Listener
	once   sync.Once
	closed chan struct{}
}

func (l *observedListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP lifecycle event")
	}
}

func awaitResult(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP lifecycle result")
	}
}
