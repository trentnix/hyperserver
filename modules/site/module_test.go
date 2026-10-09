package module_site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type memoryContacts struct{ submissions []models.ContactSubmission }

func (r *memoryContacts) Create(_ context.Context, submission *models.ContactSubmission) error {
	r.submissions = append(r.submissions, *submission)
	return nil
}

func TestModuleOwnsSiteSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var repositories []*memoryContacts
	descriptor := Module(func(got context.Context) (ContactRepository, error) {
		if got != ctx {
			t.Fatal("storage factory did not receive the initialization context")
		}
		repository := new(memoryContacts)
		repositories = append(repositories, repository)
		return repository, nil
	})
	first, second := descriptor.New().(*SiteModule), descriptor.New().(*SiteModule)
	if first == second || len(repositories) != 0 {
		t.Fatal("factory reused a module or opened storage before Init")
	}

	for _, module := range []*SiteModule{first, second} {
		app, err := server.NewApplicationServer(config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		contentManager := app.ContentManager
		if err := module.Init(ctx, app); err != nil {
			t.Fatal(err)
		}
		if app.Database != nil || app.Mail != nil || app.SessionManager != nil || app.ContentManager != contentManager {
			t.Fatal("module initialized or replaced an application service")
		}
		if err := module.Routes(routing.NewRoutes(app.Web)); err != nil {
			t.Fatal(err)
		}
		_, pattern := app.Web.Handler(httptest.NewRequest(http.MethodPost, "/contact", nil))
		if pattern != "POST /contact" {
			t.Fatalf("module did not bind contact route: %q", pattern)
		}
	}

	if len(repositories) != 2 || first.contacts != repositories[0] || second.contacts != repositories[1] {
		t.Fatal("modules did not retain their own contact repositories")
	}
	if err := first.contacts.Create(ctx, &models.ContactSubmission{Email: "first@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	if len(repositories[0].submissions) != 1 || len(repositories[1].submissions) != 0 {
		t.Fatal("a write affected the other module's storage")
	}
}

func TestSiteRegistersDefaultFactory(t *testing.T) {
	for _, descriptor := range handlers.Registered() {
		if descriptor.Name != Module(nil).Name {
			continue
		}
		module, ok := descriptor.New().(*SiteModule)
		if !ok || module.NewContacts != nil || module.contacts != nil {
			t.Fatal("registered factory did not supply an uninitialized default site")
		}
		return
	}
	t.Fatal("site did not register its default factory")
}
