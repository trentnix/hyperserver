package user

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
)

// observeResetDeletes opens another pool on the fixture database. The signal
// marks a reset transaction reaching its DELETE, without changing production code
// or replacing SQLite's locking and transaction behavior with a mock.
func observeResetDeletes(t *testing.T, db *sqlx.DB) (*sqlx.DB, <-chan struct{}) {
	t.Helper()
	var sequence int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=10000"}
	connector := &resetConnector{dsn: uri.String(), started: started}
	observed := sqlx.NewDb(sql.OpenDB(connector), "sqlite3")
	observed.SetMaxOpenConns(2)
	t.Cleanup(func() {
		if err := observed.Close(); err != nil {
			t.Error(err)
		}
	})
	return observed, started
}

type resetConnector struct {
	driver  sqlite3.SQLiteDriver
	dsn     string
	started chan<- struct{}
}

func (c *resetConnector) Driver() driver.Driver { return &c.driver }

func (c *resetConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &resetConnection{SQLiteConn: conn.(*sqlite3.SQLiteConn), started: c.started}, nil
}

type resetConnection struct {
	*sqlite3.SQLiteConn
	started chan<- struct{}
}

func (c *resetConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "DELETE FROM "+userTokenTableName) {
		select {
		case c.started <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.SQLiteConn.QueryContext(ctx, query, args)
}

func waitForResetDelete(t *testing.T, ctx context.Context, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("reset did not reach token deletion:", ctx.Err())
	}
}
