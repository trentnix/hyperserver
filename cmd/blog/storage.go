package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/trentnix/hyperserver/modules/blog"
	blogsqlite "github.com/trentnix/hyperserver/modules/blog/sqlite"
	databasesqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

type storage struct {
	posts    blog.Repository
	images   blog.ImageStore
	accounts user.AccountRepository
	sessions *session.SessionManager
}

// openStorage selects providers in one place. The application owns their lifetimes.
func openStorage(ctx context.Context, app *server.ApplicationServer, s settings) (storage, error) {
	if err := os.MkdirAll(s.dataDir, 0700); err != nil {
		return storage{}, err
	}
	path := filepath.Join(s.dataDir, "blog.db")
	pool, err := databasesqlite.Open(path)
	if err != nil {
		return storage{}, err
	}
	app.OnShutdown("blog storage", func(context.Context) error { return pool.Close() })
	posts := blogsqlite.New(pool)
	if err := posts.Initialize(ctx); err != nil {
		return storage{}, err
	}
	accounts := usersqlite.NewAccountRepository(pool)
	if err := accounts.Initialize(ctx); err != nil {
		return storage{}, err
	}

	sessions, err := session.NewSessionManager(ctx, map[string]string{"default": "sqlite"},
		map[string]session.StoreFactory{"sqlite": func(ctx context.Context) (session.SessionStore, error) {
			return sessionsqlite.NewSQLiteStore(ctx, sessionsqlite.Options{
				Connection: path, Table: "blog_sessions", SigningKey: s.sessionKey,
				TokenLifetime: 24 * time.Hour, CookieLifetime: 24 * time.Hour,
			})
		}})
	if err != nil {
		return storage{}, err
	}
	app.OnShutdown("blog sessions", func(context.Context) error { return sessions.Close() })
	return storage{posts: posts, images: posts, accounts: accounts, sessions: sessions}, nil
}
