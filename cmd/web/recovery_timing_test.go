package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
)

func TestHTTPResetResponseFloor(t *testing.T) {
	for _, stage := range []string{"lookup", "slow lookup", "lookup failure", "token storage", "slow token storage", "token storage failure", "mail", "slow mail", "mail failure", "mail timeout"} {
		for _, account := range []string{"eligible", "unknown", "other provider"} {
			for _, htmx := range []bool{false, true} {
				name := stage + "/" + account + "/native"
				if htmx {
					name = stage + "/" + account + "/htmx"
				}
				t.Run(name, func(t *testing.T) {
					var delayEnabled atomic.Bool
					var slowDelayUsed atomic.Bool
					runHTTPScenario(t, func(h *httpHarness) {
						u := h.seedUser(t, "person@example.invalid")
						email := u.Email
						if account == "unknown" {
							email = "unknown@example.invalid"
						} else if account == "other provider" {
							u.RegistrationAuthType = "other"
							if err := u.Update(context.Background(), h.app.Database); err != nil {
								t.Fatal(err)
							}
						}

						// Fake time gives exact duration assertions without wall-clock tolerances.
						synctest.Test(t, func(t *testing.T) {
							var firstWrite time.Time
							next := h.handler
							h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								firstWrite = time.Time{}
								next.ServeHTTP(&resetResponseWriter{ResponseWriter: w, firstWrite: &firstWrite}, r)
							})
							start := time.Now()
							baseline := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"baseline@example.invalid"}}, htmx)
							if baseline.Code != http.StatusOK || !strings.Contains(baseline.Body.String(), "Password reset request received.") {
								t.Fatal("missing generic acknowledgment")
							}
							if elapsed := time.Since(start); elapsed != 2*time.Second {
								t.Fatalf("unknown account duration = %v, want 2s", elapsed)
							}
							if firstWrite.Sub(start) != 2*time.Second {
								t.Fatal("unknown account wrote before the response floor")
							}
							switch stage {
							case "lookup failure":
								if _, err := h.app.Database.Exec("ALTER TABLE user RENAME TO unavailable_user"); err != nil {
									t.Fatal(err)
								}
							case "token storage failure":
								if _, err := h.app.Database.Exec(`CREATE TRIGGER fail_reset_token BEFORE INSERT ON usertoken
									BEGIN SELECT RAISE(ABORT, 'test token storage failure'); END`); err != nil {
									t.Fatal(err)
								}
							}
							h.mail.beforeSend = func(ctx context.Context) {
								if _, ok := ctx.Deadline(); ok {
									t.Error("reset handler added a processing deadline")
								}
								if stage == "mail timeout" {
									// The sender owns its timeout, independently of the response floor.
									mailCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
									defer cancel()
									<-mailCtx.Done()
									return
								}
								if stage == "slow mail" {
									time.Sleep(3 * time.Second)
									if err := ctx.Err(); err != nil {
										t.Errorf("slow delivery was canceled: %v", err)
									}
								}
								if stage == "mail" || stage == "mail failure" {
									time.Sleep(500 * time.Millisecond)
								}
							}
							if stage == "mail failure" {
								h.mail.err = errors.New("controlled delivery failure")
							}
							if stage == "mail timeout" {
								h.mail.err = context.DeadlineExceeded
							}
							delayEnabled.Store(true)

							start = time.Now()
							response := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {email}}, htmx)
							wantDuration := 2 * time.Second
							if stage == "slow lookup" || (account == "eligible" && (strings.HasPrefix(stage, "slow ") || stage == "mail timeout")) {
								wantDuration = 3 * time.Second
							}
							if elapsed := time.Since(start); elapsed != wantDuration {
								t.Fatalf("reset duration = %v, want %v", elapsed, wantDuration)
							}
							assertSameResetAcknowledgment(t, baseline, response)
							if firstWrite.Sub(start) != wantDuration {
								t.Fatal("response was written before processing and the minimum wait finished")
							}
							if strings.HasPrefix(stage, "slow ") && strings.Contains(h.logs.String(), "password reset instructions could not be sent") {
								t.Fatal("slow processing failed instead of completing normally")
							}
							delayEnabled.Store(false)
							wantMail := 0
							if account == "eligible" && stage != "lookup failure" && stage != "token storage failure" {
								wantMail = 1
							}
							if got := len(h.mail.snapshot()); got != wantMail {
								t.Fatalf("mail attempts = %d, want %d", got, wantMail)
							}
							h.assertRowCount(t, "usertoken", wantMail)
						})
					}, func(cfg *config.Config) {
						cfg.Auth.ResetMinimumResponseTime = 2 * time.Second
						const driverName = "recovery-timing-sqlite"
						sql.Register(driverName, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
							conn.RegisterAuthorizer(func(operation int, table, column, database string) int {
								if delayEnabled.Load() && ((stage == "slow lookup" && operation == sqlite3.SQLITE_READ && table == "user") ||
									(stage == "slow token storage" && operation == sqlite3.SQLITE_INSERT && table == "usertoken")) && !slowDelayUsed.Swap(true) {
									time.Sleep(3 * time.Second)
								}
								if delayEnabled.Load() && ((stage == "lookup" && operation == sqlite3.SQLITE_READ && table == "user") ||
									(stage == "token storage" && operation == sqlite3.SQLITE_INSERT && table == "usertoken")) {
									time.Sleep(100 * time.Millisecond)
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

type resetResponseWriter struct {
	http.ResponseWriter
	firstWrite *time.Time
}

func TestHTTPResetConfiguredMinimum(t *testing.T) {
	for _, minimum := range []time.Duration{0, 250 * time.Millisecond, 5 * time.Second} {
		for _, htmx := range []bool{false, true} {
			name := minimum.String() + "/native"
			if htmx {
				name = minimum.String() + "/htmx"
			}
			t.Run(name, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "person@example.invalid")
					synctest.Test(t, func(t *testing.T) {
						var firstWrite time.Time
						next := h.handler
						h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							firstWrite = time.Time{}
							next.ServeHTTP(&resetResponseWriter{ResponseWriter: w, firstWrite: &firstWrite}, r)
						})
						h.mail.beforeSend = func(ctx context.Context) {
							if _, ok := ctx.Deadline(); ok {
								t.Error("configured minimum imposed a processing deadline")
							}
							time.Sleep(500 * time.Millisecond)
						}
						var baseline *httptest.ResponseRecorder
						for _, email := range []string{"unknown@example.invalid", u.Email} {
							start := time.Now()
							response := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {email}}, htmx)
							want := minimum
							if email == u.Email {
								want = max(minimum, 500*time.Millisecond)
							}
							if time.Since(start) != want {
								t.Fatalf("response time = %v, want %v", time.Since(start), want)
							}
							if firstWrite.Sub(start) != want {
								t.Fatalf("first response write = %v, want %v", firstWrite.Sub(start), want)
							}
							if baseline == nil {
								baseline = response
							} else {
								assertSameResetAcknowledgment(t, baseline, response)
							}
						}
						if len(h.mail.snapshot()) != 1 || strings.Contains(h.logs.String(), "password reset instructions could not be sent") {
							t.Fatal("configured minimum prevented successful delivery")
						}
					})
				}, func(cfg *config.Config) {
					cfg.Auth.ResetMinimumResponseTime = minimum
				})
			})
		}
	}
}

func (w *resetResponseWriter) recordWrite() {
	if w.firstWrite.IsZero() {
		*w.firstWrite = time.Now()
	}
}

func (w *resetResponseWriter) WriteHeader(status int) {
	w.recordWrite()
	w.ResponseWriter.WriteHeader(status)
}

func (w *resetResponseWriter) Write(body []byte) (int, error) {
	w.recordWrite()
	return w.ResponseWriter.Write(body)
}

func TestHTTPResetCancellation(t *testing.T) {
	for _, stage := range []string{"lookup", "mail", "waiting", "request deadline"} {
		t.Run(stage, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "person@example.invalid")
				if stage == "lookup" {
					// Exhaust the pool so lookup must wait on the request context.
					h.app.Database.SetMaxOpenConns(1)
					conn, err := h.app.Database.Conn(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
				}
				synctest.Test(t, func(t *testing.T) {
					if stage == "mail" || stage == "request deadline" {
						h.mail.beforeSend = func(ctx context.Context) { <-ctx.Done() }
					}
					email := u.Email
					if stage == "waiting" {
						email = "unknown@example.invalid"
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					wantDuration := 250 * time.Millisecond
					if stage == "request deadline" {
						ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
						defer cancel()
						wantDuration = 3 * time.Second
					} else {
						go func() { time.Sleep(250 * time.Millisecond); cancel() }()
					}
					r := httptest.NewRequest(http.MethodPost, "/auth/reset/request/email", strings.NewReader(url.Values{"email": {email}}.Encode())).WithContext(ctx)
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					w := httptest.NewRecorder()
					var firstWrite time.Time
					start := time.Now()
					h.handler.ServeHTTP(&resetResponseWriter{ResponseWriter: w, firstWrite: &firstWrite}, r)
					if time.Since(start) != wantDuration || !firstWrite.IsZero() {
						t.Fatal("canceled request waited for the floor or wrote an acknowledgment")
					}
				})
			}, func(cfg *config.Config) {
				cfg.Auth.ResetMinimumResponseTime = 2 * time.Second
			})
		})
	}
}

func TestHTTPResetTokenStorageCancellation(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name := "native"
		if htmx {
			name = "htmx"
		}
		t.Run(name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "person@example.invalid")
				synctest.Test(t, func(t *testing.T) {
					entered, released := make(chan context.Context), make(chan struct{})
					release := sync.OnceFunc(func() { close(released) })
					defer release()
					const driverName = "recovery-cancel-sqlite"
					sql.Register(driverName, &resetCancellationDriver{
						Driver: &sqlite3.SQLiteDriver{}, entered: entered, released: released,
					})
					db, err := sql.Open(driverName, h.app.Config.Database.Connection)
					if err != nil {
						t.Fatal(err)
					}
					// Keep the sqlx object shared by the handlers, but intercept token writes.
					original := h.app.Database.DB
					h.app.Database.DB = db
					defer func() {
						h.app.Database.DB = original
						if err := db.Close(); err != nil {
							t.Error(err)
						}
					}()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					r := httptest.NewRequest(http.MethodPost, "/auth/reset/request/email", strings.NewReader(url.Values{"email": {u.Email}}.Encode())).WithContext(ctx)
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					if htmx {
						r.Header.Set("HX-Request", "true")
					}
					w := httptest.NewRecorder()
					var firstWrite time.Time
					done := make(chan struct{})
					go func() {
						defer close(done)
						h.handler.ServeHTTP(&resetResponseWriter{ResponseWriter: w, firstWrite: &firstWrite}, r)
					}()
					var storageCtx context.Context
					select {
					case storageCtx = <-entered:
					case <-done:
						t.Fatal("request did not reach token storage")
					}

					cancel()
					synctest.Wait()
					canceled := errors.Is(storageCtx.Err(), context.Canceled)
					release()
					<-done
					if !canceled {
						t.Error("request cancellation did not reach token storage")
					}
					if !firstWrite.IsZero() {
						t.Error("canceled token storage wrote an acknowledgment")
					}
				})
				h.assertRowCount(t, "usertoken", 0)
				if len(h.mail.snapshot()) != 0 {
					t.Fatal("canceled token storage attempted delivery")
				}
				h.assertUserUnchanged(t, u)
			}, func(cfg *config.Config) {
				cfg.Auth.ResetMinimumResponseTime = 2 * time.Second
			})
		})
	}
}

// Only token INSERTs block. Lookup and all other SQL use the real SQLite connection.
type resetCancellationDriver struct {
	driver.Driver
	entered  chan<- context.Context
	released <-chan struct{}
}

func (d *resetCancellationDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &resetCancellationConn{Conn: conn, gate: d}, nil
}

type resetCancellationConn struct {
	driver.Conn
	gate *resetCancellationDriver
}

func (c *resetCancellationConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "INSERT INTO usertoken") {
		c.gate.entered <- ctx
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.gate.released:
			// Let a broken context-propagation implementation finish so assertions can fail.
		}
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
