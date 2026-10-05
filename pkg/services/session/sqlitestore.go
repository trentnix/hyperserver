package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

type (
	// SQLiteStore persists session data in SQLite and signs session IDs in cookies.
	// Database initialization currently uses a process-wide connection pool.
	SQLiteStore struct {
		// JwtKey signs the session-ID cookie. It does not encrypt its contents.
		JwtKey []byte
		// lifetime of the JWT
		TokenLifetime time.Duration
		// lifetime of the cookie containing the JWT
		CookieLifetime time.Duration
		// database connection string
		connectionString string
		// table that will store sessions
		tableName string
		// database connection
		db *sqlx.DB
		// enabled
		enabled bool
	}

	// SQLiteSession is the database representation of an encoded session.
	SQLiteSession struct {
		ID         string    `db:"id"`
		Session    string    `db:"session"`
		Expires_at time.Time `db:"expires_at"`
		Created_at time.Time `db:"created_at"`
		Updated_at time.Time `db:"updated_at"`
	}
)

var (
	dbSQLiteSessionStore *sqlx.DB
	dbOnce               sync.Once

	// ErrSQLiteStoreNotConfigured indicates that SQLite session settings are missing.
	ErrSQLiteStoreNotConfigured = errors.New("the sqliteStore is not configured")
	// ErrDatabaseNotConfigured indicates that the session store has no database connection.
	ErrDatabaseNotConfigured = errors.New("the session database is not configured")
)

const (
	sqliteDriver    = "sqlite3"
	sqliteStoreName = "SQLiteStore"
)

// NewSQLiteStore validates cookie settings and prepares the SQLite store.
// The first initialized store supplies the process-wide pool reused by later stores.
func NewSQLiteStore(c *config.Config) (*SQLiteStore, error) {
	configErr := checkStoreCookieConfig(c)
	if configErr != nil {
		return nil, configErr
	}

	configOptions := getStoreConfigOptions(c, sqliteStoreName)

	sqliteStore := &SQLiteStore{
		JwtKey:           []byte(c.HTTP.Session.JwtKey),
		TokenLifetime:    c.HTTP.Session.TokenAge,
		CookieLifetime:   c.HTTP.Session.CookieAge,
		connectionString: configOptions["connection"],
		tableName:        configOptions["sessiontable"],
		enabled:          strings.EqualFold(configOptions["enabled"], "true") || configOptions["enabled"] == "1",
	}

	dbErr := sqliteStore.prepareDatabase()
	if dbErr != nil {
		return nil, dbErr
	}

	return sqliteStore, nil
}

// Get loads the session identified by the signed request cookie.
// A missing or expired cookie or stored session returns a new session. Invalid
// cookies, database failures, and corrupt unexpired data return an error.
func (s *SQLiteStore) Get(r *http.Request, name string) (*Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}

	// get the session ID from the request cookie
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	if cookie == nil {
		// if there is no cookie, return an empty session
		session := newSession(s, name)
		return session, nil
	}
	if err := validateSessionCookie(cookie); err != nil {
		return nil, NewErrInvalidToken(err)
	}

	// return the decoded Session data
	claimsData, sessionError := parseSessionJWT(jwtValue, s.JwtKey)
	if errors.Is(sessionError, jwt.ErrTokenExpired) {
		return newSession(s, name), nil
	}
	if sessionError != nil {
		return nil, sessionError
	}

	// get the session from the database
	sessionID := claimsData.ID
	session, sessionError := s.getSessionFromDatabase(r.Context(), sessionID, name)
	if sessionError != nil {
		// return an error
		return nil, sessionError
	}

	// if the session does not exist, return a new session
	if session == nil {
		session = newSession(s, name)
	}

	return session, nil
}

// New creates a new session and returns the newly created Session instance
func (s *SQLiteStore) New(r *http.Request, name string) (*Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}

	return newSession(s, name), nil
}

// Save stores session data and writes its signed identifier to an HTTP cookie.
// Updating a session does not extend its stored lifetime or recreate a deleted row.
func (s *SQLiteStore) Save(w http.ResponseWriter, r *http.Request, session *Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}
	cookie, err := s.sessionCookie(session, requestinfo.IsHTTPS(r))
	if err != nil {
		return err
	}
	if err := s.save(r.Context(), session); err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}

func (s *SQLiteStore) sessionCookie(session *Session, secure bool) (*http.Cookie, error) {
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(s.TokenLifetime)
		session.ExpiresAt = tokenExpiration
	} else {
		tokenExpiration = session.ExpiresAt
	}

	claims := &SessionClaims{
		ID:      session.ID,
		Purpose: sessionTokenPurpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(tokenExpiration),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.JwtKey)
	if err != nil {
		return nil, NewErrSessionKeyInvalid(err)
	}

	cookieAge := int(s.CookieLifetime / time.Second)

	cookie := &http.Cookie{
		Name:     session.Name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieAge,
	}
	if err := validateSessionCookie(cookie); err != nil {
		return nil, err
	}
	return cookie, nil
}

// Rotate replaces the old row and inserts a fresh session in one transaction.
// A revoked or expired session cannot be rotated back into an active session.
func (s *SQLiteStore) Rotate(w http.ResponseWriter, r *http.Request, previous *Session, data map[string]any) (*Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}
	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}
	if previous == nil || previous.ID == "" {
		return nil, NewErrSessionInvalid(nil)
	}

	next := newSession(s, previous.Name)
	for key, value := range data {
		next.Data[key] = value
	}
	encoded, err := next.EncodedData()
	if err != nil {
		return nil, err
	}
	cookie, err := s.sessionCookie(next, requestinfo.IsHTTPS(r))
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTxx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if !previous.IsNew {
		result, err := tx.ExecContext(r.Context(), "DELETE FROM "+s.tableName+" WHERE id = ? AND julianday(expires_at) > julianday(?)",
			previous.ID, time.Now())
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, NewErrSessionInvalid(nil)
		}
	}

	_, err = tx.ExecContext(r.Context(), "INSERT INTO "+s.tableName+" (id, session, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		next.ID, encoded, next.ExpiresAt, time.Now(), time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	next.IsNew = false
	http.SetCookie(w, cookie)
	return next, nil
}

// End deletes the stored session before expiring the browser cookie. Missing rows
// are already revoked. A storage failure leaves the browser cookie unchanged.
func (s *SQLiteStore) End(w http.ResponseWriter, r *http.Request, session *Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}
	if s.tableName == "" {
		return ErrSQLiteStoreNotConfigured
	}
	if session == nil || session.ID == "" {
		return NewErrSessionInvalid(nil)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName)
	if _, err := s.db.ExecContext(r.Context(), query, session.ID); err != nil {
		return err
	}
	ExpireCookie(w, r, session.Name)
	return nil
}

// IsEnabled reports whether this SQLite store is enabled in configuration.
func (s *SQLiteStore) IsEnabled() bool {
	return s.enabled
}

// prepareDatabase opens the shared pool and prepares its session table and index.
func (s *SQLiteStore) prepareDatabase() error {
	if dbSQLiteSessionStore != nil {
		s.db = dbSQLiteSessionStore
		return nil
	}

	var dbError error
	dbOnce.Do(func() {
		s.db, dbError = database.Setup(sqliteDriver, s.connectionString)
		if dbError != nil {
			return
		}

		if err := s.configureDatabase(); err != nil {
			dbError = database.NewErrDatabase(err)
			return
		}

		dbSQLiteSessionStore = s.db
	})

	if dbError != nil {
		return dbError
	}

	return nil
}

// configureDatabase prepares the session table and expiration index.
func (s *SQLiteStore) configureDatabase() error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := s.db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id VARCHAR(36) PRIMARY KEY,
				session TEXT,
				expires_at DATETIME,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`, s.tableName)
	default:
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", s.db.Driver()))
	}

	_, err := s.db.Exec(createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}
	query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_expiration ON %s (julianday(expires_at), id)", s.tableName, s.tableName)
	if _, err := s.db.Exec(query); err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
}

// getSessionFromDatabase attempts to retrieve the specified session from the database
func (s *SQLiteStore) getSessionFromDatabase(ctx context.Context, sessionID string, name string) (*Session, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}

	var dbSession SQLiteSession

	// Read only a bounded prefix so corrupt oversized rows cannot allocate their
	// entire payload in Go. loadSession rejects the extra byte before decoding.
	query := fmt.Sprintf("SELECT id, substr(CAST(session AS BLOB), 1, ?) AS session, expires_at, created_at, updated_at FROM %s WHERE id = ?", s.tableName)
	err := s.db.GetContext(ctx, &dbSession, query, maxEncodedSessionSize+1, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !dbSession.Expires_at.After(time.Now()) {
		return nil, nil
	}

	return loadSession(dbSession.ID, name, dbSession.Expires_at, s, dbSession.Session)
}

// saveSessionToDatabase creates or updates the specified session in the database
func (s *SQLiteStore) save(ctx context.Context, session *Session) error {
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}

	valueData, err := session.EncodedData()
	if err != nil {
		return err
	}

	dbSession := &SQLiteSession{
		ID:         session.ID,
		Session:    valueData,
		Expires_at: session.ExpiresAt,
	}

	var dbErr error
	if session.IsNew {
		dbErr = s.create(ctx, dbSession)
	} else {
		dbErr = s.update(ctx, dbSession)
	}

	if dbErr != nil {
		return dbErr
	}

	session.IsNew = false
	return nil
}

// create creates a new sessions table entry
func (s *SQLiteStore) create(ctx context.Context, session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	session.Created_at = time.Now()
	session.Updated_at = session.Created_at

	query := fmt.Sprintf("INSERT INTO %s (id, session, expires_at, updated_at, created_at) VALUES (:id, :session, :expires_at, :created_at, :updated_at)", s.tableName)
	_, err := s.db.NamedExecContext(ctx, query, session)

	return err
}

// update updates the specified session in the sessions table
func (s *SQLiteStore) update(ctx context.Context, session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	session.Updated_at = time.Now()

	// Saving data must not extend stored expiry or recreate a revoked session.
	query := fmt.Sprintf("UPDATE %s SET session = :session, updated_at = :updated_at WHERE id = :id", s.tableName)
	result, err := s.db.NamedExecContext(ctx, query, session)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return NewErrSessionInvalid(nil)
	}
	return nil
}
