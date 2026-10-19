package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
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

	"github.com/trentnix/hyperserver/internal/test/tlstest"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func TestRunDrainsRequestsBeforeClosingPool(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			testRunDrainsRequestsBeforeClosingPool(t, scheme)
		})
	}
}

func TestRunPreservesServicesWhenDrainingTimesOut(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	output, err := runStartupProcess(t, yaml, "shutdown-timeout", "HYPERSERVER_HTTP_SHUTDOWNTIMEOUT=1s")
	if err != nil || !strings.Contains(string(output), "drain timeout ownership passed") {
		t.Fatalf("drain timeout: error=%v output=%s", err, output)
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
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := usersqlite.NewAccountRepository(db).GetByEmail(t.Context(), "shutdown@example.invalid"); err != nil {
		t.Fatal("shutdown lost the in-flight request's write:", err)
	}
}

// checkRunShutdown runs in a subprocess to isolate registries, stdout, and cwd.
func checkRunShutdown(t *testing.T, secure bool) {
	t.Helper()
	var s *application
	started, release := make(chan struct{}), make(chan struct{})
	handlerFinished := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	var activeSession *session.Session
	moduleClosed := false
	handlers.Register(handlers.Descriptor{Name: "shutdown-test", New: func() handlers.Handler {
		return &testModule{close: func(context.Context) error {
			if activeSession == nil && os.Getenv("HS_STARTUP_TEST_HELPER") != "shutdown-timeout" {
				t.Error("module closed before active request drained")
			}
			if err := s.contactDB.Ping(); err != nil {
				t.Error("module closed after its shared pool:", err)
			}
			if activeSession != nil {
				if err := activeSession.Save(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
					t.Error("module closed after its session store:", err)
				}
			}
			moduleClosed = true
			return nil
		}}
	}})
	s = newStartupApplication(t)
	s.Config.HTTP.Port = 0
	scheme := "http"
	transport := &http.Transport{}
	if secure {
		cfg, roots := tlstest.Files(t)
		s.Config.HTTP.TLS = cfg.TLS
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
		scheme = "https"
	}
	defer transport.CloseIdleConnections()
	s.Web.HandleFunc("POST /test-shutdown", func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerFinished)
		if (r.TLS != nil) != secure {
			t.Error("request TLS state does not match the listener configuration")
		}
		close(started)
		<-release
		account := &user.User{Email: "shutdown@example.invalid"}
		if err := s.Accounts.Create(r.Context(), account); err != nil {
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
	shutdownStarted := time.Now()
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
	if err := s.contactDB.Ping(); err != nil {
		t.Fatal("run closed the pool before its active request finished:", err)
	}
	if os.Getenv("HS_STARTUP_TEST_HELPER") == "shutdown-timeout" {
		select {
		case err := <-done:
			if !errors.Is(err, server.ErrHTTPDrainIncomplete) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("unexpected shutdown error:", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("run ignored configured HTTP drain deadline")
		}
		if elapsed := time.Since(shutdownStarted); elapsed < *s.Config.HTTP.ShutdownTimeout {
			t.Fatalf("draining stopped after %v, before configured timeout %v", elapsed, *s.Config.HTTP.ShutdownTimeout)
		}
		if moduleClosed || s.contactDB.Ping() != nil {
			t.Fatal("run closed shared services while a handler was still running")
		}
		releaseRequest()
		awaitSignal(t, handlerFinished)
		// The production process exits here. The test owns cleanup after its handler stops.
		if err := s.Shutdown(); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(stdout, "drain timeout ownership passed")
		return
	}
	releaseRequest()
	awaitResult(t, response)
	awaitResult(t, done)
	if !moduleClosed {
		t.Fatal("run did not close the module")
	}
	if err := s.contactDB.Ping(); err == nil {
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

func shutdownTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestStoppingBorrowerDrainsRequestsAndPreservesPool(t *testing.T) {
	db := shutdownTestDB(t)
	owner := &server.ApplicationServer{}
	owner.OnShutdown("shared storage", func(context.Context) error { return db.Close() })
	if err := usersqlite.PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewSQLiteContactRepository(context.Background(), db)
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
	go func() { done <- server.ServeHTTP(ctx, s, closing, 10*time.Second) }()
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
	if err := usersqlite.NewAccountRepository(db).Create(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	releaseRequest()
	awaitResult(t, response)
	awaitResult(t, done)
	account = &user.User{Email: "after@example.invalid"}
	if err := usersqlite.NewAccountRepository(db).Create(context.Background(), account); err != nil {
		t.Fatal("stopping the site closed the account consumer's pool:", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM hyperserver_contact_submission`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("active request did not persist before shutdown: count = %d, error = %v", count, err)
	}
	if err := owner.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err == nil {
		t.Fatal("application shutdown left its pool open")
	}
}

func TestServeHTTPReturnsListenerFailure(t *testing.T) {
	want := errors.New("accept failed")
	listener := &failedListener{err: want}
	if err := server.ServeHTTP(context.Background(), &http.Server{}, listener, time.Second); !errors.Is(err, want) {
		t.Fatalf("serve error = %v, want %v", err, want)
	}
}

func TestServeHTTPDrainDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseRequest := func() { once.Do(func() { close(release) }) }
	defer releaseRequest()
	requestContext := make(chan context.Context, 1)
	s := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		requestContext <- r.Context()
		close(started)
		<-release // Deliberately ignore cancellation to prove shutdown stays bounded.
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.ServeHTTP(ctx, s, listener, 30*time.Millisecond) }()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		res, err := client.Get("http://" + listener.Addr().String())
		if res != nil {
			res.Body.Close()
		}
		response <- err
	}()
	awaitSignal(t, started)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, server.ErrHTTPDrainIncomplete) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("drain timeout error:", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown ignored drain deadline")
	}
	awaitSignal(t, (<-requestContext).Done())
	select {
	case err := <-response:
		if err == nil {
			t.Error("client connection remained open after forced close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client connection did not close")
	}
	releaseRequest()
	awaitSignal(t, finished)
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
