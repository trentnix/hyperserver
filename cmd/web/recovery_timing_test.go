package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
)

// This characterizes the synchronous timing leak, not the desired contract.
// When recovery moves off the request path, storage and mail gates must not
// hold up the acknowledgment for any valid email address.
func TestHTTPResetTimingBaseline(t *testing.T) {
	for _, stage := range []string{"lookup", "token storage", "mail", "mail failure"} {
		for _, account := range []string{"eligible", "unknown", "other provider"} {
			for _, htmx := range []bool{false, true} {
				name := stage + "/" + account + "/native"
				if htmx {
					name = stage + "/" + account + "/htmx"
				}
				t.Run(name, func(t *testing.T) {
					gate := newRecoveryGate()
					runHTTPScenario(t, func(h *httpHarness) {
						defer gate.release()
						email := "unknown@example.invalid"
						if account != "unknown" {
							u := h.seedUser(t, "person@example.invalid")
							email = u.Email
							if account == "other provider" {
								u.RegistrationAuthType = "other"
								if err := u.Update(context.Background(), h.app.Database); err != nil {
									t.Fatal(err)
								}
							}
						}
						// Warm schema setup before measuring the recovery operation.
						baseline := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"baseline@example.invalid"}}, htmx)
						if baseline.Code != http.StatusOK || !strings.Contains(baseline.Body.String(), "Password reset request received.") {
							t.Fatal("missing generic acknowledgment")
						}
						if stage == "mail" || stage == "mail failure" {
							h.mail.beforeSend = gate.wait
						}
						if stage == "mail failure" {
							h.mail.err = errors.New("controlled delivery failure")
						}
						gate.enabled.Store(true)

						pending := startRecoveryRequest(t, h, email, htmx)
						shouldBlock := stage == "lookup" || account == "eligible"
						if shouldBlock {
							select {
							case <-gate.entered:
							case <-pending.done:
								t.Fatal("request finished without reaching the controlled delay")
							case <-pending.ctx.Done():
								t.Fatal("request did not reach the controlled delay")
							}
							select {
							case <-pending.wrote:
								t.Fatal("baseline wrote an acknowledgment before recovery work finished")
							case <-pending.done:
								t.Fatal("baseline returned before recovery work finished")
							default:
							}
							gate.release()
						}

						select {
						case <-pending.done:
						case <-pending.ctx.Done():
							t.Fatal("request did not finish")
						}
						if !shouldBlock {
							select {
							case <-gate.entered:
								t.Fatal("unknown or ineligible account attempted token storage or mail")
							default:
							}
						}
						assertSameResetAcknowledgment(t, baseline, pending.recorder)
						wantMail := 0
						if account == "eligible" {
							wantMail = 1
						}
						if got := len(h.mail.snapshot()); got != wantMail {
							t.Fatalf("mail attempts = %d, want %d", got, wantMail)
						}
						h.assertRowCount(t, "usertoken", wantMail)
					}, func(cfg *config.Config) {
						// The configure callback runs only in the isolated child process.
						// Keep the real SQLite driver so schema setup and SQL still execute.
						const driverName = "recovery-timing-sqlite"
						sql.Register(driverName, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
							conn.RegisterAuthorizer(func(operation int, table, column, database string) int {
								if (stage == "lookup" && operation == sqlite3.SQLITE_READ && table == "user") ||
									(stage == "token storage" && operation == sqlite3.SQLITE_INSERT && table == "usertoken") {
									gate.wait()
								}
								return sqlite3.SQLITE_OK
							})
							return nil
						}})
						cfg.Database.Driver = driverName
					})
				})
			}
		}
	}
}

// Channels expose the dependency on slow I/O without sleep-based timing thresholds.
type recoveryGate struct {
	enabled     atomic.Bool
	entered     chan struct{}
	released    chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newRecoveryGate() *recoveryGate {
	return &recoveryGate{entered: make(chan struct{}), released: make(chan struct{})}
}

func (g *recoveryGate) wait() {
	if !g.enabled.Load() {
		return
	}
	g.enterOnce.Do(func() { close(g.entered) })
	<-g.released
}

func (g *recoveryGate) release() {
	g.releaseOnce.Do(func() { close(g.released) })
}

type pendingRecovery struct {
	recorder  *httptest.ResponseRecorder
	ctx       context.Context
	wrote     chan struct{}
	done      chan struct{}
	writeOnce sync.Once
}

func (p *pendingRecovery) Header() http.Header {
	return p.recorder.Header()
}

func (p *pendingRecovery) WriteHeader(status int) {
	p.writeOnce.Do(func() { close(p.wrote) })
	p.recorder.WriteHeader(status)
}

func (p *pendingRecovery) Write(body []byte) (int, error) {
	p.writeOnce.Do(func() { close(p.wrote) })
	return p.recorder.Write(body)
}

func startRecoveryRequest(t *testing.T, h *httpHarness, email string, htmx bool) *pendingRecovery {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	request := httptest.NewRequest(http.MethodPost, h.baseURL+"/auth/reset/request/email", strings.NewReader(url.Values{"email": {email}}.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	pending := &pendingRecovery{recorder: httptest.NewRecorder(), ctx: ctx, wrote: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(pending.done)
		h.handler.ServeHTTP(pending, request)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-pending.done:
		case <-time.After(5 * time.Second):
			t.Error("recovery request did not stop")
		}
	})
	return pending
}
