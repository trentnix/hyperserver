package module_site

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
)

var (
	_ ContactRepository = (*storage.SQLiteContactRepository)(nil)
	_ ContactRepository = (*storage.FileContactRepository)(nil)
)

func TestSiteInitializesFileStorageWithoutSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &server.ApplicationServer{Config: &config.Config{}, ContentManager: content.NewContentManager()}
	called := false
	module := &SiteModule{NewContacts: func(got context.Context) (ContactRepository, error) {
		called = true
		if got != ctx {
			t.Fatal("module did not propagate its initialization context")
		}
		return storage.NewFileContactRepository(got, filepath.Join(t.TempDir(), "contacts"))
	}}
	if err := module.Init(ctx, app); err != nil {
		t.Fatal(err)
	}
	if !called || module.contacts == nil {
		t.Fatal("module did not initialize its selected provider")
	}
	if err := module.contacts.Create(ctx, &models.ContactSubmission{Email: "person@example.invalid"}); err != nil {
		t.Fatal("file-backed module required SQL:", err)
	}
}

func TestSiteRejectsFailedContactProvider(t *testing.T) {
	want := errors.New("provider unavailable")
	for _, providerErr := range []error{nil, want} {
		module := &SiteModule{NewContacts: func(context.Context) (ContactRepository, error) {
			return nil, providerErr
		}}
		app := &server.ApplicationServer{Config: &config.Config{}, ContentManager: content.NewContentManager()}
		err := module.Init(context.Background(), app)
		if err == nil || (providerErr != nil && !errors.Is(err, providerErr)) {
			t.Fatalf("provider failure = %v, want %v", err, providerErr)
		}
		if module.contacts != nil {
			t.Fatal("failed initialization published a contact provider")
		}
	}
}

func TestSiteRejectsIncompatibleSQLiteContacts(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), `
		CREATE TABLE hyperserver_contact_submission (id TEXT);
		CREATE TABLE hyperserver_contact_version (version uint64, dirty bool);
		INSERT INTO hyperserver_contact_version VALUES (1, false);
	`); err != nil {
		t.Fatal(err)
	}
	module := &SiteModule{NewContacts: func(ctx context.Context) (ContactRepository, error) {
		return storage.NewSQLiteContactRepository(ctx, db)
	}}
	app := &server.ApplicationServer{Config: &config.Config{}, ContentManager: content.NewContentManager()}
	if err := module.Init(t.Context(), app); err == nil || !strings.Contains(err.Error(), "missing email") {
		t.Fatalf("incompatible contact schema did not stop initialization: %v", err)
	}
	if module.contacts != nil {
		t.Fatal("failed initialization published a contact provider")
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal("failed initialization closed the borrowed pool:", err)
	}
}

func TestSiteChecksRequirementsBeforeOpeningStorage(t *testing.T) {
	for _, app := range []*server.ApplicationServer{
		{},
		{Config: &config.Config{}},
	} {
		called := false
		module := &SiteModule{NewContacts: func(context.Context) (ContactRepository, error) {
			called = true
			return nil, errors.New("storage must not be opened")
		}}
		if err := module.Init(context.Background(), app); err == nil || called {
			t.Fatalf("missing requirement: error=%v, storage opened=%t", err, called)
		}
		if module.contacts != nil {
			t.Fatal("failed initialization published storage")
		}
	}
}

func TestSiteRequiresExplicitContactStorage(t *testing.T) {
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	module := Module(nil, nil).New().(*SiteModule)
	err = module.Init(t.Context(), app)
	if err == nil || err.Error() != "site requires a contact-storage factory" {
		t.Fatalf("missing factory: %v", err)
	}
	if module.contacts != nil || module.contentManager != nil {
		t.Fatal("failed initialization installed site state")
	}
}
