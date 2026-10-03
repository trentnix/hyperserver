package module_site

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
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
		return models.NewFileContactRepository(got, filepath.Join(t.TempDir(), "contacts"))
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
		// No other services are needed when storage initialization fails.
		err := module.Init(context.Background(), &server.ApplicationServer{})
		if err == nil || (providerErr != nil && !errors.Is(err, providerErr)) {
			t.Fatalf("provider failure = %v, want %v", err, providerErr)
		}
		if module.contacts != nil {
			t.Fatal("failed initialization published a contact provider")
		}
	}
}
