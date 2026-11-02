package blog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// assemblyApplication supplies services without opening storage or initializing modules.
func assemblyApplication() *application.Application {
	cfg := &config.Config{}
	cfg.Auth.Enabled = true
	return &application.Application{
		ApplicationServer: &server.ApplicationServer{Config: cfg, ContentManager: contentservice.NewContentManager()},
		Services:          application.Services{Accounts: unavailableAccounts{}},
	}
}

// TestRegisteredBlogRequiresSite keeps catalog-only selection from bypassing assembly.
func TestRegisteredBlogRequiresSite(t *testing.T) {
	for _, descriptor := range handlers.Registered() {
		if descriptor.Name == "blog" {
			err := descriptor.New().Init(t.Context(), assemblyApplication().ApplicationServer)
			if err == nil || !strings.Contains(err.Error(), "Site.Configure") {
				t.Fatalf("unconfigured blog initialization: %v", err)
			}
			return
		}
	}
	t.Fatal("blog missing from the module catalog")
}

// TestSiteAssembly checks shared ownership within a site and isolation between sites.
func TestSiteAssembly(t *testing.T) {
	first, second := &Site{}, &Site{}
	var modules []*module
	for _, site := range []*Site{first, second} {
		app := assemblyApplication()
		descriptor, err := site.Configure(app)
		if err != nil {
			t.Fatal(err)
		}
		m := descriptor.New().(*module)
		another := descriptor.New().(*module)
		if m == another || m.settings == another.settings {
			t.Fatal("descriptor reused a module instance or its settings")
		}
		policy := site.AuthenticationPolicy()
		if policy.AllowAccount == nil || policy.BeforeRegister == nil || policy.AfterLogin == nil {
			t.Fatal("site omitted authentication policy")
		}
		if descriptor.Name != "blog" || m.access != &site.access || m.profile != &site.profile || m.site != site {
			t.Fatal("site did not share its own state with its descriptors")
		}
		m.profile.value = Profile{Name: "Shared profile"}
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if app.ContentManager.BuildLayoutData(r).(navigation).Profile.Name != "Shared profile" {
			t.Fatal("rendering does not use the module's profile")
		}
		if _, err := site.Configure(assemblyApplication()); err == nil {
			t.Fatal("site was reused across applications")
		}
		modules = append(modules, m)
	}
	if modules[0].access == modules[1].access || modules[0].profile == modules[1].profile {
		t.Fatal("sites share mutable state")
	}
}

// TestSiteAssemblyFailures rejects missing services and conflicting rendering owners.
func TestSiteAssemblyFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*application.Application)
		want    string
	}{
		{"authentication disabled", func(app *application.Application) { app.Config.Auth.Enabled = false }, "requires authentication"},
		{"accounts absent", func(app *application.Application) { app.Accounts = nil }, "account storage"},
		{"rendering selected", func(app *application.Application) {
			if err := app.ContentManager.ApplyDefaults("other", func(*contentservice.ContentManagerService) {}); err != nil {
				t.Fatal(err)
			}
		}, "defaults"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, site := assemblyApplication(), &Site{}
			tc.prepare(app)
			descriptor, err := site.Configure(app)
			if err == nil || !strings.Contains(err.Error(), tc.want) || descriptor.New != nil {
				t.Fatalf("descriptor=%v error=%v, want %q", descriptor, err, tc.want)
			}
			if site.configured || site.populateNavigation != nil || site.access.setup != nil {
				t.Fatal("failed assembly published initialized state")
			}
		})
	}
}
