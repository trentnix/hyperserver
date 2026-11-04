package application_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

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
	repository := usersqlite.NewAccountRepository(db)
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
		submission := &user.User{Email: "person@example.invalid"}
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
				err = errors.New("in-flight request failed to save its account")
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

	// The account consumer still uses the same pool while the borrower is stopping.
	account := &user.User{Email: "before@example.invalid"}
	if err := usersqlite.NewAccountRepository(db).Create(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	releaseRequest()
	awaitResult(t, response)
	awaitResult(t, done)
	account = &user.User{Email: "after@example.invalid"}
	if err := usersqlite.NewAccountRepository(db).Create(context.Background(), account); err != nil {
		t.Fatal("stopping the borrower closed the account consumer's pool:", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM user`).Scan(&count); err != nil || count != 3 {
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
