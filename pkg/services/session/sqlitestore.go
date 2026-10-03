package session

import (
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
// A missing or expired cookie or missing row returns a new session. Invalid cookies,
// database failures, and corrupt data return an error. Stored expiry is not checked.
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
	session, sessionError := s.getSessionFromDatabase(sessionID, name)
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

// Save serializes specified Session to the database and saves the session identifier
// to an HTTP cookie
func (s *SQLiteStore) Save(w http.ResponseWriter, r *http.Request, session *Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}

	// create a token with just the session (and expiration)
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
		return NewErrSessionKeyInvalid(err)
	}

	cookieAge := int(s.CookieLifetime / time.Second)

	// save the session to the database
	saveErr := s.save(session)
	if saveErr != nil {
		return saveErr
	}

	// write the cookie with the session token
	http.SetCookie(w, &http.Cookie{
		Name:     session.Name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieAge,
	})

	return nil
}

// End expires the browser cookie. It does not delete or expire the SQLite row,
// so a captured cookie is not revoked by this method.
func (s *SQLiteStore) End(w http.ResponseWriter, r *http.Request, session *Session) error {
	ExpireCookie(w, r, session.Name)
	return nil
}

// IsEnabled reports whether this SQLite store is enabled in configuration.
func (s *SQLiteStore) IsEnabled() bool {
	return s.enabled
}

// validateDatabase determines whether the sessionsTable exists and, if not, it creates it
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

		tableExists, err := database.TableExists(s.db.DB, s.tableName)
		if err != nil {
			dbError = database.NewErrDatabaseConfiguration(err)
			return
		}

		if !tableExists {
			err = s.configureDatabase()
			if err != nil {
				dbError = database.NewErrDatabase(err)
				return
			}
		}

		dbSQLiteSessionStore = s.db
	})

	if dbError != nil {
		return dbError
	}

	return nil
}

// configureDatabase creates the sessionTable according to the specified database vendor
func (s *SQLiteStore) configureDatabase() error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := s.db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
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

	return nil
}

// getSessionFromDatabase attempts to retrieve the specified session from the database
func (s *SQLiteStore) getSessionFromDatabase(sessionID string, name string) (*Session, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}

	var dbSession SQLiteSession

	query := fmt.Sprintf("SELECT id, session, expires_at, created_at, updated_at FROM %s WHERE id = ?", s.tableName)
	err := s.db.Get(&dbSession, query, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return loadSession(dbSession.ID, name, dbSession.Expires_at, s, dbSession.Session)
}

// saveSessionToDatabase creates or updates the specified session in the database
func (s *SQLiteStore) save(session *Session) error {
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
		dbErr = s.create(dbSession)
	} else {
		dbErr = s.update(dbSession)
	}

	if dbErr != nil {
		return dbErr
	}

	session.IsNew = false
	return nil
}

// create creates a new sessions table entry
func (s *SQLiteStore) create(session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	session.Created_at = time.Now()
	session.Updated_at = session.Created_at

	query := fmt.Sprintf("INSERT INTO %s (id, session, expires_at, updated_at, created_at) VALUES (:id, :session, :expires_at, :created_at, :updated_at)", s.tableName)
	_, err := s.db.NamedExec(query, session)

	return err
}

// update updates the specified session in the sessions table
func (s *SQLiteStore) update(session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	session.Updated_at = time.Now()

	query := fmt.Sprintf("UPDATE %s SET session = :session, expires_at = :expires_at, updated_at = :updated_at WHERE id = :id", s.tableName)
	_, err := s.db.NamedExec(query, session)

	return err
}

// end updates the database to terminate a session by setting the expiration time
// to the current time
func (s *SQLiteStore) end(session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	query := fmt.Sprintf("UPDATE %s SET expires_at = :expires_at WHERE id = :id", s.tableName)
	_, err := s.db.NamedExec(query, map[string]interface{}{
		"expires_at": time.Now(),
		"id":         session.ID,
	})

	return err
}

// delete deletes the specified session from the sessions table
func (s *SQLiteStore) delete(session *SQLiteSession) error {
	if session.ID == "" {
		return NewErrSessionInvalid(nil)
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName)
	_, err := s.db.Exec(query, session.ID)

	return err
}
