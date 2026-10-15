package module_site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
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
	client := new(messaging.MailClient)
	descriptor := Module(func(got context.Context) (ContactRepository, error) {
		if got != ctx {
			t.Fatal("storage factory did not receive the initialization context")
		}
		repository := new(memoryContacts)
		repositories = append(repositories, repository)
		return repository, nil
	}, client)
	first, second := descriptor.New().(*SiteModule), descriptor.New().(*SiteModule)
	if first == second || len(repositories) != 0 || first.mailClient != client || second.mailClient != client {
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
		if app.ContentManager != contentManager || module.mailClient != client {
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
		if descriptor.Name != Module(nil, nil).Name {
			continue
		}
		module, ok := descriptor.New().(*SiteModule)
		if !ok || module.NewContacts != nil || module.contacts != nil || module.mailClient != nil {
			t.Fatal("registered factory did not supply an uninitialized default site")
		}
		return
	}
	t.Fatal("site did not register its default factory")
}

func TestSiteWithoutMailReportsDeliveryFailure(t *testing.T) {
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	module := Module(func(context.Context) (ContactRepository, error) {
		return new(memoryContacts), nil
	}, nil).New().(*SiteModule)
	if err := module.Init(t.Context(), app); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/test-email?to=recipient@example.invalid", nil)
	response := httptest.NewRecorder()
	module.TestEmail(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "mail client is not configured\n" {
		t.Fatalf("missing mail response: %d %s", response.Code, response.Body.String())
	}
}

func TestSiteLeavesNotificationSelectionToApplication(t *testing.T) {
	for _, mode := range []string{"unconfigured", "custom"} {
		t.Run(mode, func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{RenderNotifications: true}})
			if err != nil {
				t.Fatal(err)
			}
			customCalled := false
			if mode == "custom" {
				app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
					customCalled = true
					return nil, nil, nil
				}
			}
			module := Module(func(context.Context) (ContactRepository, error) { return new(memoryContacts), nil }, nil).New()
			if err := module.Init(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			load := app.ContentManager.LoadNotifications
			if mode == "unconfigured" {
				if load != nil {
					t.Fatal("module initialization installed a notification loader")
				}
				return
			}
			if load == nil {
				t.Fatal("enabled notifications did not install a loader")
			}
			_, _, err = load(httptest.NewRequest(http.MethodGet, "/", nil))
			if mode == "custom" && (err != nil || !customCalled) {
				t.Fatal("site replaced the application's notification loader")
			}
		})
	}
}
