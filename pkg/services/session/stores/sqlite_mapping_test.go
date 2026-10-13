package stores

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

func readSQLiteSessionRow(t *testing.T, store *SQLiteStore, id string) SQLiteSession {
	t.Helper()
	var row SQLiteSession
	err := store.db.QueryRowContext(t.Context(), `SELECT session, expires_at, id, updated_at, created_at FROM sessions WHERE id = ?`, id).
		Scan(&row.Session, &row.Expires_at, &row.ID, &row.Updated_at, &row.Created_at)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func assertSQLiteSessionRow(t *testing.T, got, want SQLiteSession) {
	t.Helper()
	if got.ID != want.ID || got.Session != want.Session || !got.Expires_at.Equal(want.Expires_at) || !got.Created_at.Equal(want.Created_at) || !got.Updated_at.Equal(want.Updated_at) {
		t.Fatalf("stored session = %+v, want %+v", got, want)
	}
}

func TestSQLiteSessionFieldMappings(t *testing.T) {
	store := revocationStore(t)
	value := sessions.NewSession(store, "mapping")
	value.Data["text"] = "O'Neil'); DROP TABLE sessions; --"
	encoded, err := value.EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	row := SQLiteSession{ID: value.ID, Session: encoded, Expires_at: time.Now().Add(time.Hour)}
	started := time.Now()
	if err := store.create(t.Context(), &row); err != nil {
		t.Fatal(err)
	}
	if row.Created_at.Before(started) || row.Created_at.After(time.Now()) || !row.Created_at.Equal(row.Updated_at) {
		t.Fatalf("incorrect creation timestamps: %+v", row)
	}
	assertSQLiteSessionRow(t, readSQLiteSessionRow(t, store, row.ID), row)

	// Distinct stored timestamps expose a scan or update that confuses their roles.
	row.Created_at = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	row.Updated_at = time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC)
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET created_at = ?, updated_at = ? WHERE id = ?`, row.Created_at, row.Updated_at, row.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.getSessionFromDatabase(t.Context(), row.ID, value.Name)
	if err != nil || loaded == nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.ID != row.ID || loaded.Name != value.Name || loaded.IsNew || loaded.Store != store || !loaded.ExpiresAt.Equal(row.Expires_at) || loaded.Data["text"] != value.Data["text"] {
		t.Fatalf("incorrect loaded session: %+v", loaded)
	}

	loaded.Data["text"] = "replacement"
	row.Session, err = loaded.EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	// Updating payload data must not overwrite the stored lifetime or creation time.
	update := row
	update.Expires_at = row.Expires_at.Add(time.Hour)
	update.Created_at = time.Now()
	started = time.Now()
	if err := store.update(t.Context(), &update); err != nil {
		t.Fatal(err)
	}
	if update.Updated_at.Before(started) || update.Updated_at.After(time.Now()) {
		t.Fatalf("incorrect update time: %v", update.Updated_at)
	}
	row.Updated_at = update.Updated_at
	assertSQLiteSessionRow(t, readSQLiteSessionRow(t, store, row.ID), row)
}

func TestSQLiteSessionScanFailure(t *testing.T) {
	for _, column := range []string{"session", "expires_at", "created_at", "updated_at"} {
		t.Run(column, func(t *testing.T) {
			store := revocationStore(t)
			value, request := savedSession(t, store)
			if _, err := store.db.ExecContext(t.Context(), "UPDATE sessions SET "+column+" = NULL WHERE id = ?", value.ID); err != nil {
				t.Fatal(err)
			}
			if got, err := store.Get(request, value.Name); got != nil || err == nil {
				t.Fatalf("corrupt row returned a session: %v, %v", got, err)
			}
		})
	}
}

func TestSQLiteSessionCancellationWhileWaiting(t *testing.T) {
	for _, operation := range []string{"read", "create", "update", "rotate", "end", "cleanup"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := revocationStore(t)
				store.db.SetMaxOpenConns(1)
				value, request := savedSession(t, store)
				if operation == "cleanup" {
					if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour), value.ID); err != nil {
						t.Fatal(err)
					}
				}
				before := readSQLiteSessionRow(t, store, value.ID)
				conn, err := store.db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				ctx, cancel := context.WithCancel(request.Context())
				defer cancel()
				request = request.WithContext(ctx)
				writer := httptest.NewRecorder()
				result := make(chan error, 1)
				var got *sessions.Session
				var deleted int64
				go func() {
					var err error
					switch operation {
					case "read":
						got, err = store.Get(request, value.Name)
					case "create":
						err = store.Save(writer, request, sessions.NewSession(store, "new"))
					case "update":
						value.Data["identity"] = "replacement"
						err = store.Save(writer, request, value)
					case "rotate":
						got, err = store.Rotate(writer, request, value, map[string]any{"identity": "replacement"})
					case "end":
						err = store.End(writer, request, value)
					case "cleanup":
						deleted, err = store.DeleteExpired(ctx, 1)
					}
					result <- err
				}()
				synctest.Wait()
				select {
				case err := <-result:
					t.Fatalf("operation did not wait for the connection: %v", err)
				default:
				}

				cancel()
				synctest.Wait()
				select {
				case err = <-result:
				default:
					conn.Close()
					<-result
					t.Fatal("cancellation did not interrupt the storage operation")
				}
				if !errors.Is(err, context.Canceled) || got != nil || deleted != 0 || len(writer.Result().Cookies()) != 0 {
					t.Fatalf("canceled operation returned success or changed cookies: %v", err)
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}

				assertSQLiteSessionRow(t, readSQLiteSessionRow(t, store, before.ID), before)
				var count int
				if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("canceled operation changed stored rows: count=%d, error=%v", count, err)
				}
			})
		})
	}
}
